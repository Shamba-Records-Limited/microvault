package utils

import (
	"bytes"
	"fmt"

	"github.com/ledongthuc/pdf"
	"github.com/pdfcpu/pdfcpu/pkg/api"
	"github.com/pdfcpu/pdfcpu/pkg/pdfcpu/model"
)

// PDFParser handles PDF document parsing
type PDFParser struct{}

// NewPDFParser creates a new PDF parser
func NewPDFParser() *PDFParser {
	return &PDFParser{}
}

// DecryptPDF decrypts a password-protected PDF
func (p *PDFParser) DecryptPDF(pdfData []byte, password string) ([]byte, error) {
	// Use pdfcpu to decrypt
	reader := bytes.NewReader(pdfData)
	var buf bytes.Buffer

	// Create configuration with password
	conf := model.NewDefaultConfiguration()
	conf.UserPW = password
	conf.OwnerPW = password

	err := api.Decrypt(reader, &buf, conf)
	if err != nil {
		return nil, fmt.Errorf("failed to decrypt PDF: %w", err)
	}

	return buf.Bytes(), nil
}

// ExtractText extracts text from PDF
func (p *PDFParser) ExtractText(pdfData []byte) (string, error) {
	reader := bytes.NewReader(pdfData)

	r, err := pdf.NewReader(reader, int64(len(pdfData)))
	if err != nil {
		return "", fmt.Errorf("failed to create PDF reader: %w", err)
	}

	var buf bytes.Buffer
	numPages := r.NumPage()

	for pageNum := 1; pageNum <= numPages; pageNum++ {
		page := r.Page(pageNum)
		if page.V.IsNull() {
			continue
		}

		text, err := page.GetPlainText(nil)
		if err != nil {
			// Continue with other pages if one fails
			continue
		}

		buf.WriteString(text)
		buf.WriteString("\n\n")
	}

	return buf.String(), nil
}

// ParseWithPassword attempts to parse a password-protected PDF
func (p *PDFParser) ParseWithPassword(pdfData []byte, password string) (string, error) {
	// First try to decrypt
	decryptedData, err := p.DecryptPDF(pdfData, password)
	if err != nil {
		return "", fmt.Errorf("decryption failed: %w", err)
	}

	// Then extract text
	text, err := p.ExtractText(decryptedData)
	if err != nil {
		return "", fmt.Errorf("text extraction failed: %w", err)
	}

	return text, nil
}
