package docs

import (
	"archive/zip"
	"bytes"
	"fmt"
	"strings"
)

// docxDoc builds a simple Word document out of paragraphs.
type docxDoc struct {
	body strings.Builder
}

// textWidthTwips is the page width inside 0.75-inch margins.
const textWidthTwips = 12240 - 2*1080

type run struct {
	text  string
	bold  bool
	size  int // half-points (22 = 11pt)
	color string
}

type para struct {
	runs       []run
	before     int // space before, twips
	after      int
	rightTab   bool // a tab stop at the right margin (for dates)
	bullet     bool
	bottomRule bool
	keepNext   bool
}

func (d *docxDoc) add(p para) {
	b := &d.body
	b.WriteString("<w:p><w:pPr>")
	if p.keepNext {
		b.WriteString("<w:keepNext/>")
	}
	if p.bottomRule {
		b.WriteString(`<w:pBdr><w:bottom w:val="single" w:sz="6" w:space="1" w:color="1A1A1E"/></w:pBdr>`)
	}
	if p.rightTab {
		fmt.Fprintf(b, `<w:tabs><w:tab w:val="right" w:pos="%d"/></w:tabs>`, textWidthTwips)
	}
	fmt.Fprintf(b, `<w:spacing w:before="%d" w:after="%d" w:line="264" w:lineRule="auto"/>`, p.before, p.after)
	if p.bullet {
		b.WriteString(`<w:ind w:left="300" w:hanging="220"/>`)
	}
	b.WriteString("</w:pPr>")
	runs := p.runs
	if p.bullet {
		runs = append([]run{{text: "•\t", size: runs[0].size}}, runs...)
		// The tab after the bullet lines the text up with the hanging indent.
	}
	for _, r := range runs {
		b.WriteString("<w:r><w:rPr>")
		b.WriteString(`<w:rFonts w:ascii="Calibri" w:hAnsi="Calibri" w:cs="Calibri"/>`)
		if r.bold {
			b.WriteString("<w:b/>")
		}
		if r.color != "" {
			fmt.Fprintf(b, `<w:color w:val="%s"/>`, r.color)
		}
		size := r.size
		if size == 0 {
			size = 21
		}
		fmt.Fprintf(b, `<w:sz w:val="%d"/><w:szCs w:val="%d"/>`, size, size)
		b.WriteString("</w:rPr>")
		parts := strings.Split(r.text, "\t")
		for i, part := range parts {
			if i > 0 {
				b.WriteString("<w:tab/>")
			}
			if part != "" {
				fmt.Fprintf(b, `<w:t xml:space="preserve">%s</w:t>`, xmlEscape(part))
			}
		}
		b.WriteString("</w:r>")
	}
	b.WriteString("</w:p>")
}

func (d *docxDoc) bytes() ([]byte, error) {
	var out bytes.Buffer
	zw := zip.NewWriter(&out)
	files := []struct{ name, body string }{
		{"[Content_Types].xml", `<?xml version="1.0" encoding="UTF-8" standalone="yes"?>
<Types xmlns="http://schemas.openxmlformats.org/package/2006/content-types"><Default Extension="rels" ContentType="application/vnd.openxmlformats-package.relationships+xml"/><Default Extension="xml" ContentType="application/xml"/><Override PartName="/word/document.xml" ContentType="application/vnd.openxmlformats-officedocument.wordprocessingml.document.main+xml"/></Types>`},
		{"_rels/.rels", `<?xml version="1.0" encoding="UTF-8" standalone="yes"?>
<Relationships xmlns="http://schemas.openxmlformats.org/package/2006/relationships"><Relationship Id="rId1" Type="http://schemas.openxmlformats.org/officeDocument/2006/relationships/officeDocument" Target="word/document.xml"/></Relationships>`},
		{"word/document.xml", `<?xml version="1.0" encoding="UTF-8" standalone="yes"?>
<w:document xmlns:w="http://schemas.openxmlformats.org/wordprocessingml/2006/main"><w:body>` + d.body.String() +
			`<w:sectPr><w:pgSz w:w="12240" w:h="15840"/><w:pgMar w:top="1008" w:right="1080" w:bottom="1008" w:left="1080" w:header="720" w:footer="720" w:gutter="0"/></w:sectPr></w:body></w:document>`},
	}
	for _, file := range files {
		writer, err := zw.Create(file.name)
		if err != nil {
			return nil, err
		}
		if _, err := writer.Write([]byte(file.body)); err != nil {
			return nil, err
		}
	}
	if err := zw.Close(); err != nil {
		return nil, err
	}
	return out.Bytes(), nil
}

func xmlEscape(s string) string {
	var b strings.Builder
	for _, r := range s {
		switch r {
		case '&':
			b.WriteString("&amp;")
		case '<':
			b.WriteString("&lt;")
		case '>':
			b.WriteString("&gt;")
		case '"':
			b.WriteString("&quot;")
		default:
			if r < 0x20 && r != '\t' && r != '\n' && r != '\r' {
				continue
			}
			b.WriteRune(r)
		}
	}
	return b.String()
}
