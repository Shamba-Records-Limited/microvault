package mpesa

import (
	"context"
	"encoding/base64"
	"net/http"
	"strconv"

	pkgErrors "github.com/Shamba-Records-Limited/microvault/pkg/errors"
)

const pathDynamicQR = "/mpesa/qrcode/v1/generate"

// TrxCode selects what a dynamic QR code does when scanned.
type TrxCode string

// The QR transaction types.
const (
	TrxBuyGoods       TrxCode = "BG"
	TrxPayBill        TrxCode = "PB"
	TrxWithdrawAgent  TrxCode = "WA"
	TrxSendMoney      TrxCode = "SM"
	TrxSendToBusiness TrxCode = "SB"
)

// QRRequest asks Daraja for a scannable code. The Go field names are readable;
// the wire names are Safaricom's.
type QRRequest struct {
	MerchantName string
	ReferenceNo  string
	AmountKES    int64
	TrxCode      TrxCode

	// CreditPartyIdentifier is the paybill, till or number being paid.
	CreditPartyIdentifier string

	// Size is the image edge in pixels. Defaults to 300.
	Size string
}

// QRResponse carries the generated code.
type QRResponse struct {
	ResponseCode        string `json:"ResponseCode"`
	RequestID           string `json:"RequestID"`
	ResponseDescription string `json:"ResponseDescription"`

	// QRCode is a base64-encoded PNG. It is returned as it arrived: a library
	// has no business writing files, deriving paths from data, or depending on
	// the process working directory.
	QRCode string `json:"QRCode"`
}

// DecodePNG decodes the returned image to bytes. It touches no filesystem; if a
// caller wants a file, a caller can write one.
func (q QRResponse) DecodePNG() ([]byte, error) {
	decoded, err := base64.StdEncoding.DecodeString(q.QRCode)
	if err != nil {
		return nil, mpesaErr("qr_decode").
			Code(pkgErrors.CodeDecodeFailed).
			Wrapf(err, "QR code is not valid base64")
	}
	return decoded, nil
}

// DynamicQR generates a scannable payment code.
func (c *Client) DynamicQR(ctx context.Context, req QRRequest) (*QRResponse, error) {
	errb := mpesaErr("dynamic_qr")

	if req.MerchantName == "" || req.CreditPartyIdentifier == "" {
		return nil, errb.
			Code(pkgErrors.CodeMissingDependency).
			Errorf("merchant name and credit party identifier are required")
	}
	if req.TrxCode == "" {
		req.TrxCode = TrxPayBill
	}
	if req.AmountKES <= 0 {
		return nil, errb.Code(pkgErrors.CodeInvalidAmount).Errorf("amount must be positive")
	}
	if req.Size == "" {
		req.Size = "300"
	}

	body := map[string]any{
		"MerchantName": req.MerchantName,
		"RefNo":        req.ReferenceNo,
		"Amount":       strconv.FormatInt(req.AmountKES, 10),
		"TrxCode":      string(req.TrxCode),
		"CPI":          req.CreditPartyIdentifier,
		"Size":         req.Size,
	}
	return call[QRResponse](ctx, c, errb, http.MethodPost, pathDynamicQR, body)
}
