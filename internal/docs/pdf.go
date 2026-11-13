// Package docs lays out résumés and cover letters as PDF and Word files.
//
// The PDFs use the built-in Helvetica fonts and real text (no images), so
// the hiring software employers use to read résumés can read every word.
package docs

import (
	"bytes"
	"compress/zlib"
	"fmt"
	"strings"
	"unicode/utf8"
)

const (
	pageWidth  = 612.0 // US Letter, in points
	pageHeight = 792.0
)

// pdfDoc builds a simple text PDF, page by page.
type pdfDoc struct {
	margin float64
	pages  []*bytes.Buffer
	page   *bytes.Buffer
	y      float64 // baseline of the next line, from the bottom
}

func newPDF(margin float64) *pdfDoc {
	doc := &pdfDoc{margin: margin}
	doc.newPage()
	return doc
}

func (d *pdfDoc) newPage() {
	d.page = &bytes.Buffer{}
	d.pages = append(d.pages, d.page)
	d.y = pageHeight - d.margin
}

func (d *pdfDoc) width() float64 { return pageWidth - 2*d.margin }

// need starts a new page when the next `height` points won't fit.
func (d *pdfDoc) need(height float64) {
	if d.y-height < d.margin {
		d.newPage()
	}
}

func (d *pdfDoc) gap(points float64) { d.y -= points }

// text draws one line at x on the current baseline.
func (d *pdfDoc) text(x float64, font fontFace, size float64, gray float64, s string) {
	fmt.Fprintf(d.page, "BT /%s %.2f Tf %.3f g 1 0 0 1 %.2f %.2f Tm (%s) Tj ET\n", font.ref, size, gray, x, d.y, pdfString(s))
}

// rule draws a thin horizontal line just under the current baseline.
func (d *pdfDoc) rule(offset float64) {
	fmt.Fprintf(d.page, "0.6 w 0.1 0.1 0.12 RG %.2f %.2f m %.2f %.2f l S\n", d.margin, d.y-offset, pageWidth-d.margin, d.y-offset)
}

// paragraph wraps text to the width and draws it, moving down line by line.
func (d *pdfDoc) paragraph(x, width float64, font fontFace, size, leading, gray float64, s string) {
	for _, line := range wrap(s, font, size, width) {
		d.need(leading)
		d.y -= leading
		d.text(x, font, size, gray, line)
	}
}

// bytes assembles the finished file.
func (d *pdfDoc) bytes() []byte {
	var out bytes.Buffer
	var offsets []int
	object := func(body string) {
		offsets = append(offsets, out.Len())
		fmt.Fprintf(&out, "%d 0 obj\n%s\nendobj\n", len(offsets), body)
	}
	out.WriteString("%PDF-1.4\n%\xe2\xe3\xcf\xd3\n")

	// 1 catalog, 2 pages, 3 regular font, 4 bold font, then each page and its content.
	firstPage := 5
	kids := make([]string, len(d.pages))
	for i := range d.pages {
		kids[i] = fmt.Sprintf("%d 0 R", firstPage+2*i)
	}
	object("<< /Type /Catalog /Pages 2 0 R >>")
	object(fmt.Sprintf("<< /Type /Pages /Kids [%s] /Count %d >>", strings.Join(kids, " "), len(d.pages)))
	object("<< /Type /Font /Subtype /Type1 /BaseFont /Helvetica /Encoding /WinAnsiEncoding >>")
	object("<< /Type /Font /Subtype /Type1 /BaseFont /Helvetica-Bold /Encoding /WinAnsiEncoding >>")
	for i, page := range d.pages {
		object(fmt.Sprintf("<< /Type /Page /Parent 2 0 R /MediaBox [0 0 %.0f %.0f] /Resources << /Font << /F1 3 0 R /F2 4 0 R >> >> /Contents %d 0 R >>",
			pageWidth, pageHeight, firstPage+2*i+1))
		var packed bytes.Buffer
		writer := zlib.NewWriter(&packed)
		_, _ = writer.Write(page.Bytes())
		_ = writer.Close()
		offsets = append(offsets, out.Len())
		fmt.Fprintf(&out, "%d 0 obj\n<< /Length %d /Filter /FlateDecode >>\nstream\n", len(offsets), packed.Len())
		out.Write(packed.Bytes())
		out.WriteString("\nendstream\nendobj\n")
	}

	xref := out.Len()
	fmt.Fprintf(&out, "xref\n0 %d\n0000000000 65535 f \n", len(offsets)+1)
	for _, offset := range offsets {
		fmt.Fprintf(&out, "%010d 00000 n \n", offset)
	}
	fmt.Fprintf(&out, "trailer\n<< /Size %d /Root 1 0 R >>\nstartxref\n%d\n%%%%EOF\n", len(offsets)+1, xref)
	return out.Bytes()
}

// fontFace is one of the two built-in fonts, with its character widths
// (thousandths of the font size, from the standard Adobe metrics).
type fontFace struct {
	ref    string
	widths map[rune]int
}

var regular = fontFace{ref: "F1", widths: helveticaWidths}
var bold = fontFace{ref: "F2", widths: helveticaBoldWidths}

func (f fontFace) measure(s string, size float64) float64 {
	total := 0
	for _, r := range s {
		total += f.charWidth(r)
	}
	return float64(total) * size / 1000
}

func (f fontFace) charWidth(r rune) int {
	if width, ok := f.widths[r]; ok {
		return width
	}
	if base, ok := accentBase[r]; ok {
		if width, ok := f.widths[base]; ok {
			return width
		}
	}
	switch r {
	case '•':
		return 350
	case '–', '€':
		return 556
	case '—':
		return 1000
	case '‘', '’':
		return 222
	case '“', '”':
		return 333
	case '…':
		return 1000
	}
	return 556
}

// wrap breaks text into lines no wider than width.
func wrap(s string, font fontFace, size, width float64) []string {
	var lines []string
	for _, paragraph := range strings.Split(s, "\n") {
		words := strings.Fields(paragraph)
		if len(words) == 0 {
			lines = append(lines, "")
			continue
		}
		line := words[0]
		for _, word := range words[1:] {
			candidate := line + " " + word
			if font.measure(candidate, size) <= width {
				line = candidate
				continue
			}
			lines = append(lines, line)
			line = word
		}
		lines = append(lines, line)
	}
	return lines
}

// pdfString encodes text for a PDF string in WinAnsi (Windows-1252), the
// encoding the built-in fonts use, and escapes the special characters.
func pdfString(s string) string {
	var b strings.Builder
	for _, r := range s {
		var c byte
		switch {
		case r == '(' || r == ')' || r == '\\':
			b.WriteByte('\\')
			b.WriteRune(r)
			continue
		case r >= 32 && r < 127:
			c = byte(r)
		case r >= 0xA0 && r <= 0xFF:
			c = byte(r)
		default:
			if mapped, ok := winAnsiExtra[r]; ok {
				c = mapped
			} else if r == utf8.RuneError || r < 32 {
				c = ' '
			} else {
				c = '?'
			}
		}
		if c >= 0x80 {
			fmt.Fprintf(&b, "\\%03o", c)
		} else {
			b.WriteByte(c)
		}
	}
	return b.String()
}

var winAnsiExtra = map[rune]byte{
	'€': 0x80, '‚': 0x82, '„': 0x84, '…': 0x85, '‘': 0x91, '’': 0x92, '“': 0x93, '”': 0x94,
	'•': 0x95, '–': 0x96, '—': 0x97, '™': 0x99, '\t': ' ',
}

// accentBase maps accented letters to the plain letter with the same width.
var accentBase = map[rune]rune{}

func init() {
	groups := map[rune]string{
		'A': "ÀÁÂÃÄÅ", 'C': "Ç", 'E': "ÈÉÊË", 'I': "ÌÍÎÏ", 'N': "Ñ", 'O': "ÒÓÔÕÖØ", 'U': "ÙÚÛÜ", 'Y': "Ý",
		'a': "àáâãäå", 'c': "ç", 'e': "èéêë", 'i': "ìíîï", 'n': "ñ", 'o': "òóôõöø", 'u': "ùúûü", 'y': "ýÿ",
	}
	for base, letters := range groups {
		for _, letter := range letters {
			accentBase[letter] = base
		}
	}
}

func widthTable(chars string, widths []int) map[rune]int {
	table := map[rune]int{}
	i := 0
	for _, r := range chars {
		table[r] = widths[i]
		i++
	}
	return table
}

const asciiChars = " !\"#$%&'()*+,-./0123456789:;<=>?@ABCDEFGHIJKLMNOPQRSTUVWXYZ[\\]^_`abcdefghijklmnopqrstuvwxyz{|}~"

var helveticaWidths = widthTable(asciiChars, []int{
	278, 278, 355, 556, 556, 889, 667, 191, 333, 333, 389, 584, 278, 333, 278, 278,
	556, 556, 556, 556, 556, 556, 556, 556, 556, 556, 278, 278, 584, 584, 584, 556, 1015,
	667, 667, 722, 722, 667, 611, 778, 722, 278, 500, 667, 556, 833, 722, 778, 667, 778, 722, 667, 611, 722, 667, 944, 667, 667, 611,
	278, 278, 278, 469, 556, 333,
	556, 556, 500, 556, 556, 278, 556, 556, 222, 222, 500, 222, 833, 556, 556, 556, 556, 333, 500, 278, 556, 500, 722, 500, 500, 500,
	334, 260, 334, 584,
})

var helveticaBoldWidths = widthTable(asciiChars, []int{
	278, 333, 474, 556, 556, 889, 722, 238, 333, 333, 389, 584, 278, 333, 278, 278,
	556, 556, 556, 556, 556, 556, 556, 556, 556, 556, 333, 333, 584, 584, 584, 611, 975,
	722, 722, 722, 722, 667, 611, 778, 722, 278, 556, 722, 611, 833, 722, 778, 667, 778, 722, 667, 611, 722, 667, 944, 667, 667, 611,
	333, 278, 333, 584, 556, 333,
	556, 611, 556, 611, 556, 333, 611, 611, 278, 278, 556, 278, 889, 611, 611, 611, 611, 389, 556, 333, 611, 556, 778, 556, 556, 500,
	389, 280, 389, 584,
})
