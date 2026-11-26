package pdf

import (
	"bytes"
	"compress/zlib"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"os"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"unicode/utf16"
)

// ExtractText reads a PDF file and returns its extracted text, with pages
// separated by form-feed ('\f') and lines ordered top-to-bottom, left-to-right.
// It uses only the Go standard library.
func ExtractText(path string) (string, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return "", fmt.Errorf("read pdf %s: %w", path, err)
	}
	return ExtractTextFromBytes(data)
}

// ExtractTextFromBytes extracts layout-ordered text from raw PDF bytes.
func ExtractTextFromBytes(data []byte) (string, error) {
	objs := parseObjects(data)
	if len(objs) == 0 {
		return "", errors.New("no PDF indirect objects found")
	}

	pageIDs := collectPageObjectIDs(data, objs)
	if len(pageIDs) == 0 {
		return "", errors.New("no PDF pages found")
	}

	// Cache decoded ToUnicode CMaps per font object ID.
	cmapCache := make(map[int]*toUnicodeMap)

	var pages []string
	for _, pageID := range pageIDs {
		pageBody, ok := objs[pageID]
		if !ok {
			continue
		}
		fontMap := resolvePageFonts(pageBody, objs, cmapCache)
		contentIDs := resolvePageContents(pageBody, objs)

		var fragments []textFragment
		for _, cid := range contentIDs {
			cBody, ok := objs[cid]
			if !ok {
				continue
			}
			streamBytes, err := decodeStream(cBody)
			if err != nil {
				continue
			}
			fragments = append(fragments, parseContentStream(streamBytes, fontMap)...)
		}
		pages = append(pages, layoutFragments(fragments))
	}

	return strings.Join(pages, "\f\n"), nil
}

var (
	reObjStart = regexp.MustCompile(`(?m)(\d+)\s+\d+\s+obj\b`)
	reRef      = regexp.MustCompile(`(\d+)\s+\d+\s+R\b`)
)

// parseObjects indexes all indirect objects "N G obj ... endobj" by object number.
func parseObjects(data []byte) map[int][]byte {
	objs := make(map[int][]byte)
	locs := reObjStart.FindAllSubmatchIndex(data, -1)
	for _, loc := range locs {
		objNum, err := strconv.Atoi(string(data[loc[2]:loc[3]]))
		if err != nil {
			continue
		}
		bodyStart := loc[1]
		endIdx := bytes.Index(data[bodyStart:], []byte("endobj"))
		if endIdx < 0 {
			continue
		}
		objs[objNum] = data[bodyStart : bodyStart+endIdx]
	}
	return objs
}

// collectPageObjectIDs walks the /Pages tree from /Root if possible, falling
// back to all /Type /Page objects in object-ID order.
func collectPageObjectIDs(data []byte, objs map[int][]byte) []int {
	if rootID := findRootPagesID(data, objs); rootID > 0 {
		var out []int
		visited := make(map[int]bool)
		walkPages(rootID, objs, visited, &out)
		if len(out) > 0 {
			return out
		}
	}

	var ids []int
	for id, body := range objs {
		if isPageDict(body) {
			ids = append(ids, id)
		}
	}
	sort.Ints(ids)
	return ids
}

func findRootPagesID(data []byte, objs map[int][]byte) int {
	reRoot := regexp.MustCompile(`/Root\s+(\d+)\s+\d+\s+R`)
	m := reRoot.FindSubmatch(data)
	if m == nil {
		return 0
	}
	catID, _ := strconv.Atoi(string(m[1]))
	catBody, ok := objs[catID]
	if !ok {
		return 0
	}
	rePages := regexp.MustCompile(`/Pages\s+(\d+)\s+\d+\s+R`)
	pm := rePages.FindSubmatch(catBody)
	if pm == nil {
		return 0
	}
	pagesID, _ := strconv.Atoi(string(pm[1]))
	return pagesID
}

func walkPages(nodeID int, objs map[int][]byte, visited map[int]bool, out *[]int) {
	if visited[nodeID] {
		return
	}
	visited[nodeID] = true
	body, ok := objs[nodeID]
	if !ok {
		return
	}
	if isPageDict(body) {
		*out = append(*out, nodeID)
		return
	}
	kidsIdx := bytes.Index(body, []byte("/Kids"))
	if kidsIdx < 0 {
		return
	}
	rest := body[kidsIdx:]
	openB := bytes.IndexByte(rest, '[')
	closeB := bytes.IndexByte(rest, ']')
	if openB < 0 || closeB < openB {
		return
	}
	for _, m := range reRef.FindAllSubmatch(rest[openB:closeB+1], -1) {
		kidID, _ := strconv.Atoi(string(m[1]))
		walkPages(kidID, objs, visited, out)
	}
}

func isPageDict(body []byte) bool {
	// Match /Type /Page but not /Type /Pages
	re := regexp.MustCompile(`/Type\s*/Page\b`)
	return re.Match(body) && !bytes.Contains(body, []byte("/Pages"))
}

func resolvePageContents(pageBody []byte, objs map[int][]byte) []int {
	idx := bytes.Index(pageBody, []byte("/Contents"))
	if idx < 0 {
		return nil
	}
	rest := bytes.TrimSpace(pageBody[idx+len("/Contents"):])
	if len(rest) == 0 {
		return nil
	}
	if rest[0] == '[' {
		closeB := bytes.IndexByte(rest, ']')
		if closeB < 0 {
			return nil
		}
		var ids []int
		for _, m := range reRef.FindAllSubmatch(rest[:closeB+1], -1) {
			id, _ := strconv.Atoi(string(m[1]))
			ids = append(ids, id)
		}
		return ids
	}
	if m := reRef.FindSubmatch(rest); m != nil {
		id, _ := strconv.Atoi(string(m[1]))
		return []int{id}
	}
	return nil
}

func resolvePageFonts(pageBody []byte, objs map[int][]byte, cache map[int]*toUnicodeMap) map[string]*toUnicodeMap {
	result := make(map[string]*toUnicodeMap)
	resDict := extractSubDict(pageBody, []byte("/Resources"), objs)
	if resDict == nil {
		return result
	}
	fontDict := extractSubDict(resDict, []byte("/Font"), objs)
	if fontDict == nil {
		return result
	}

	reFontEntry := regexp.MustCompile(`/([A-Za-z0-9_+.-]+)\s+(\d+)\s+\d+\s+R`)
	for _, m := range reFontEntry.FindAllSubmatch(fontDict, -1) {
		name := string(m[1])
		fontObjID, _ := strconv.Atoi(string(m[2]))
		if cmap, ok := cache[fontObjID]; ok {
			result[name] = cmap
			continue
		}
		cmap := loadFontToUnicode(fontObjID, objs)
		cache[fontObjID] = cmap
		result[name] = cmap
	}
	return result
}

func extractSubDict(dictBytes []byte, key []byte, objs map[int][]byte) []byte {
	idx := bytes.Index(dictBytes, key)
	if idx < 0 {
		return nil
	}
	rest := bytes.TrimSpace(dictBytes[idx+len(key):])
	if bytes.HasPrefix(rest, []byte("<<")) {
		return sliceDict(rest)
	}
	if m := reRef.FindSubmatch(rest); m != nil {
		id, _ := strconv.Atoi(string(m[1]))
		return objs[id]
	}
	return nil
}

func sliceDict(b []byte) []byte {
	depth := 0
	for i := 0; i+1 < len(b); i++ {
		if b[i] == '<' && b[i+1] == '<' {
			depth++
			i++
		} else if b[i] == '>' && b[i+1] == '>' {
			depth--
			i++
			if depth == 0 {
				return b[:i+1]
			}
		}
	}
	return b
}

type toUnicodeMap struct {
	charMap map[uint32]string
	codeLen int
}

func loadFontToUnicode(fontObjID int, objs map[int][]byte) *toUnicodeMap {
	fontBody, ok := objs[fontObjID]
	if !ok {
		return nil
	}
	idx := bytes.Index(fontBody, []byte("/ToUnicode"))
	if idx < 0 {
		return nil
	}
	m := reRef.FindSubmatch(fontBody[idx:])
	if m == nil {
		return nil
	}
	cmapObjID, _ := strconv.Atoi(string(m[1]))
	cmapObj, ok := objs[cmapObjID]
	if !ok {
		return nil
	}
	raw, err := decodeStream(cmapObj)
	if err != nil {
		return nil
	}
	return parseToUnicodeCMap(raw)
}

func parseToUnicodeCMap(raw []byte) *toUnicodeMap {
	m := &toUnicodeMap{
		charMap: make(map[uint32]string),
		codeLen: 1,
	}

	// Detect code length from codespacerange if present (e.g. <00><FF> vs <0000><FFFF>)
	reCS := regexp.MustCompile(`begincodespacerange\s*<([0-9A-Fa-f]+)>\s*<([0-9A-Fa-f]+)>`)
	if cs := reCS.FindSubmatch(raw); cs != nil {
		if l := len(cs[1]) / 2; l > 0 {
			m.codeLen = l
		}
	}

	// Parse beginbfchar ... endbfchar
	reBFCharBlock := regexp.MustCompile(`(?s)beginbfchar(.*?)endbfchar`)
	reHexPair := regexp.MustCompile(`<([0-9A-Fa-f]+)>\s*<([0-9A-Fa-f]+)>`)
	for _, blk := range reBFCharBlock.FindAllSubmatch(raw, -1) {
		for _, pair := range reHexPair.FindAllSubmatch(blk[1], -1) {
			src, ok := parseHexUint(pair[1])
			if !ok {
				continue
			}
			if l := len(pair[1]) / 2; l > m.codeLen {
				m.codeLen = l
			}
			m.charMap[src] = decodeUTF16Hex(pair[2])
		}
	}

	// Parse beginbfrange ... endbfrange
	reBFRangeBlock := regexp.MustCompile(`(?s)beginbfrange(.*?)endbfrange`)
	reRange := regexp.MustCompile(`<([0-9A-Fa-f]+)>\s*<([0-9A-Fa-f]+)>\s*<([0-9A-Fa-f]+)>`)
	for _, blk := range reBFRangeBlock.FindAllSubmatch(raw, -1) {
		for _, r := range reRange.FindAllSubmatch(blk[1], -1) {
			start, ok1 := parseHexUint(r[1])
			end, ok2 := parseHexUint(r[2])
			dstStart, ok3 := parseHexUint(r[3])
			if !ok1 || !ok2 || !ok3 || end < start {
				continue
			}
			for c := start; c <= end; c++ {
				val := dstStart + (c - start)
				m.charMap[c] = string(rune(val))
			}
		}
	}

	return m
}

func parseHexUint(b []byte) (uint32, bool) {
	v, err := strconv.ParseUint(string(b), 16, 32)
	if err != nil {
		return 0, false
	}
	return uint32(v), true
}

func decodeUTF16Hex(hexBytes []byte) string {
	raw, err := hex.DecodeString(string(hexBytes))
	if err != nil || len(raw) == 0 {
		return ""
	}
	if len(raw)%2 != 0 {
		return string(raw)
	}
	u16 := make([]uint16, len(raw)/2)
	for i := range u16 {
		u16[i] = uint16(raw[2*i])<<8 | uint16(raw[2*i+1])
	}
	return string(utf16.Decode(u16))
}

func (m *toUnicodeMap) decode(raw []byte) string {
	if m == nil || len(m.charMap) == 0 {
		return decodeWinAnsi(raw)
	}
	var sb strings.Builder
	step := m.codeLen
	if step <= 0 {
		step = 1
	}
	for i := 0; i < len(raw); {
		if i+step <= len(raw) {
			var code uint32
			for j := 0; j < step; j++ {
				code = (code << 8) | uint32(raw[i+j])
			}
			if s, ok := m.charMap[code]; ok {
				sb.WriteString(s)
				i += step
				continue
			}
		}
		if s, ok := m.charMap[uint32(raw[i])]; ok {
			sb.WriteString(s)
		} else {
			sb.WriteByte(raw[i])
		}
		i++
	}
	return sb.String()
}

func decodeWinAnsi(raw []byte) string {
	var sb strings.Builder
	for _, b := range raw {
		sb.WriteRune(rune(b))
	}
	return sb.String()
}

func decodeStream(objBody []byte) ([]byte, error) {
	idx := bytes.Index(objBody, []byte("stream"))
	if idx < 0 {
		return nil, errors.New("no stream in object")
	}
	header := objBody[:idx]
	start := idx + len("stream")
	if start < len(objBody) && objBody[start] == '\r' {
		start++
	}
	if start < len(objBody) && objBody[start] == '\n' {
		start++
	}
	end := bytes.LastIndex(objBody[start:], []byte("endstream"))
	if end < 0 {
		return nil, errors.New("no endstream")
	}
	streamData := objBody[start : start+end]

	if bytes.Contains(header, []byte("/FlateDecode")) {
		// Try exact slice first, then trim at most one trailing EOL before endstream
		// (never TrimRight all \r\n, as compressed zlib bytes can end in 0x0a/0x0d).
		candidates := [][]byte{streamData}
		if bytes.HasSuffix(streamData, []byte("\r\n")) {
			candidates = append(candidates, streamData[:len(streamData)-2])
		} else if bytes.HasSuffix(streamData, []byte("\n")) || bytes.HasSuffix(streamData, []byte("\r")) {
			candidates = append(candidates, streamData[:len(streamData)-1])
		}
		var lastErr error
		for _, cand := range candidates {
			zr, err := zlib.NewReader(bytes.NewReader(cand))
			if err != nil {
				lastErr = err
				continue
			}
			out, err := io.ReadAll(zr)
			zr.Close()
			if err == nil || len(out) > 0 {
				return out, nil
			}
			lastErr = err
		}
		return nil, lastErr
	}
	return streamData, nil
}

type textFragment struct {
	x    float64
	y    float64
	text string
}

// parseContentStream scans PDF text operators (BT/ET, Tf, Td/TD/Tm/T*, Tj/TJ/'/")
// and returns positioned text fragments.
func parseContentStream(data []byte, fonts map[string]*toUnicodeMap) []textFragment {
	var fragments []textFragment
	var inText bool
	var curFont *toUnicodeMap
	var x, y float64
	var lineX, lineY float64
	var leading float64

	var args []pdfToken
	toks := tokenizeContent(data)

	for _, tok := range toks {
		if tok.kind != tokIdent {
			args = append(args, tok)
			continue
		}

		op := tok.str
		switch op {
		case "BT":
			inText = true
			x, y = 0, 0
			lineX, lineY = 0, 0
		case "ET":
			inText = false
		case "Tf":
			if len(args) >= 2 && args[len(args)-2].kind == tokName {
				curFont = fonts[args[len(args)-2].str]
			}
		case "TL":
			if len(args) >= 1 {
				if v, ok := args[len(args)-1].asFloat(); ok {
					leading = v
				}
			}
		case "Td":
			if inText && len(args) >= 2 {
				tx, ok1 := args[len(args)-2].asFloat()
				ty, ok2 := args[len(args)-1].asFloat()
				if ok1 && ok2 {
					lineX += tx
					lineY += ty
					x, y = lineX, lineY
				}
			}
		case "TD":
			if inText && len(args) >= 2 {
				tx, ok1 := args[len(args)-2].asFloat()
				ty, ok2 := args[len(args)-1].asFloat()
				if ok1 && ok2 {
					leading = -ty
					lineX += tx
					lineY += ty
					x, y = lineX, lineY
				}
			}
		case "Tm":
			if inText && len(args) >= 6 {
				tx, ok1 := args[len(args)-2].asFloat()
				ty, ok2 := args[len(args)-1].asFloat()
				if ok1 && ok2 {
					lineX, lineY = tx, ty
					x, y = tx, ty
				}
			}
		case "T*":
			if inText {
				lineY -= leading
				x, y = lineX, lineY
			}
		case "Tj":
			if inText && len(args) >= 1 && args[len(args)-1].kind == tokString {
				s := curFont.decode(args[len(args)-1].raw)
				if s != "" {
					fragments = append(fragments, textFragment{x: x, y: y, text: s})
				}
			}
		case "'":
			if inText && len(args) >= 1 && args[len(args)-1].kind == tokString {
				lineY -= leading
				x, y = lineX, lineY
				s := curFont.decode(args[len(args)-1].raw)
				if s != "" {
					fragments = append(fragments, textFragment{x: x, y: y, text: s})
				}
			}
		case "\"":
			if inText && len(args) >= 3 && args[len(args)-1].kind == tokString {
				lineY -= leading
				x, y = lineX, lineY
				s := curFont.decode(args[len(args)-1].raw)
				if s != "" {
					fragments = append(fragments, textFragment{x: x, y: y, text: s})
				}
			}
		case "TJ":
			if inText && len(args) >= 1 && args[len(args)-1].kind == tokArray {
				var sb strings.Builder
				for _, item := range args[len(args)-1].arr {
					if item.kind == tokString {
						sb.WriteString(curFont.decode(item.raw))
					} else if v, ok := item.asFloat(); ok && v < -180 {
						sb.WriteByte(' ')
					}
				}
				if s := sb.String(); s != "" {
					fragments = append(fragments, textFragment{x: x, y: y, text: s})
				}
			}
		}
		args = args[:0]
	}

	return fragments
}

type tokKind int

const (
	tokIdent tokKind = iota
	tokName
	tokNumber
	tokString
	tokArray
)

type pdfToken struct {
	kind tokKind
	str  string
	raw  []byte
	arr  []pdfToken
}

func (t pdfToken) asFloat() (float64, bool) {
	if t.kind != tokNumber {
		return 0, false
	}
	v, err := strconv.ParseFloat(t.str, 64)
	return v, err == nil
}

func tokenizeContent(data []byte) []pdfToken {
	var out []pdfToken
	i := 0
	n := len(data)
	for i < n {
		c := data[i]
		if isWS(c) {
			i++
			continue
		}
		if c == '%' {
			for i < n && data[i] != '\n' && data[i] != '\r' {
				i++
			}
			continue
		}
		if c == '(' {
			raw, next := readLiteralString(data, i)
			out = append(out, pdfToken{kind: tokString, raw: raw})
			i = next
			continue
		}
		if c == '<' {
			if i+1 < n && data[i+1] == '<' {
				i += 2
				continue
			}
			raw, next := readHexString(data, i)
			out = append(out, pdfToken{kind: tokString, raw: raw})
			i = next
			continue
		}
		if c == '>' && i+1 < n && data[i+1] == '>' {
			i += 2
			continue
		}
		if c == '[' {
			arr, next := readArray(data, i)
			out = append(out, pdfToken{kind: tokArray, arr: arr})
			i = next
			continue
		}
		if c == '/' {
			i++
			start := i
			for i < n && !isDelim(data[i]) && !isWS(data[i]) {
				i++
			}
			out = append(out, pdfToken{kind: tokName, str: string(data[start:i])})
			continue
		}
		start := i
		for i < n && !isDelim(data[i]) && !isWS(data[i]) {
			i++
		}
		word := string(data[start:i])
		if isNumber(word) {
			out = append(out, pdfToken{kind: tokNumber, str: word})
		} else {
			out = append(out, pdfToken{kind: tokIdent, str: word})
		}
	}
	return out
}

func readArray(data []byte, start int) ([]pdfToken, int) {
	i := start + 1
	n := len(data)
	var arr []pdfToken
	for i < n {
		c := data[i]
		if isWS(c) {
			i++
			continue
		}
		if c == ']' {
			return arr, i + 1
		}
		if c == '(' {
			raw, next := readLiteralString(data, i)
			arr = append(arr, pdfToken{kind: tokString, raw: raw})
			i = next
			continue
		}
		if c == '<' {
			raw, next := readHexString(data, i)
			arr = append(arr, pdfToken{kind: tokString, raw: raw})
			i = next
			continue
		}
		s := i
		for i < n && !isDelim(data[i]) && !isWS(data[i]) {
			i++
		}
		if i > s {
			w := string(data[s:i])
			if isNumber(w) {
				arr = append(arr, pdfToken{kind: tokNumber, str: w})
			} else {
				arr = append(arr, pdfToken{kind: tokIdent, str: w})
			}
		} else {
			i++
		}
	}
	return arr, i
}

func readLiteralString(data []byte, start int) ([]byte, int) {
	var buf []byte
	depth := 1
	i := start + 1
	n := len(data)
	for i < n && depth > 0 {
		c := data[i]
		if c == '\\' && i+1 < n {
			i++
			esc := data[i]
			switch esc {
			case 'n':
				buf = append(buf, '\n')
			case 'r':
				buf = append(buf, '\r')
			case 't':
				buf = append(buf, '\t')
			case 'b':
				buf = append(buf, '\b')
			case 'f':
				buf = append(buf, '\f')
			case '(', ')', '\\':
				buf = append(buf, esc)
			case '\r':
				if i+1 < n && data[i+1] == '\n' {
					i++
				}
			case '\n':
				// line continuation
			default:
				if esc >= '0' && esc <= '7' {
					oct := int(esc - '0')
					for k := 0; k < 2 && i+1 < n && data[i+1] >= '0' && data[i+1] <= '7'; k++ {
						i++
						oct = oct*8 + int(data[i]-'0')
					}
					buf = append(buf, byte(oct))
				} else {
					buf = append(buf, esc)
				}
			}
			i++
			continue
		}
		if c == '(' {
			depth++
			buf = append(buf, c)
		} else if c == ')' {
			depth--
			if depth > 0 {
				buf = append(buf, c)
			}
		} else {
			buf = append(buf, c)
		}
		i++
	}
	return buf, i
}

func readHexString(data []byte, start int) ([]byte, int) {
	i := start + 1
	n := len(data)
	var hexDigits []byte
	for i < n && data[i] != '>' {
		if !isWS(data[i]) {
			hexDigits = append(hexDigits, data[i])
		}
		i++
	}
	if i < n && data[i] == '>' {
		i++
	}
	if len(hexDigits)%2 == 1 {
		hexDigits = append(hexDigits, '0')
	}
	raw, err := hex.DecodeString(string(hexDigits))
	if err != nil {
		return nil, i
	}
	return raw, i
}

func isWS(c byte) bool {
	return c == ' ' || c == '\t' || c == '\r' || c == '\n' || c == '\f' || c == 0
}

func isDelim(c byte) bool {
	return c == '(' || c == ')' || c == '<' || c == '>' || c == '[' || c == ']' || c == '{' || c == '}' || c == '/' || c == '%'
}

func isNumber(s string) bool {
	if s == "" {
		return false
	}
	_, err := strconv.ParseFloat(s, 64)
	return err == nil
}

// layoutFragments sorts text fragments into lines by Y (top-to-bottom) and X (left-to-right).
func layoutFragments(frags []textFragment) string {
	if len(frags) == 0 {
		return ""
	}
	sort.SliceStable(frags, func(i, j int) bool {
		if abs(frags[i].y-frags[j].y) > 2.5 {
			return frags[i].y > frags[j].y
		}
		return frags[i].x < frags[j].x
	})

	var lines []string
	var curLine strings.Builder
	curY := frags[0].y
	for _, f := range frags {
		if abs(f.y-curY) > 2.5 {
			lines = append(lines, strings.TrimSpace(curLine.String()))
			curLine.Reset()
			curY = f.y
		} else if curLine.Len() > 0 && !strings.HasSuffix(curLine.String(), " ") && !strings.HasPrefix(f.text, " ") {
			curLine.WriteByte(' ')
		}
		curLine.WriteString(f.text)
	}
	if curLine.Len() > 0 {
		lines = append(lines, strings.TrimSpace(curLine.String()))
	}
	return strings.Join(lines, "\n")
}

func abs(v float64) float64 {
	if v < 0 {
		return -v
	}
	return v
}
