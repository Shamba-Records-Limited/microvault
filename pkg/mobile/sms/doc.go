// Package sms sends outbound SMS messages and ingests delivery-report callbacks.
//
// SMSService is the entry point. Build it with NewSMSService, then register one
// or more SMSProvider implementations with RegisterProvider; the service holds
// them by name and resolves them at send time. SMSRequest carries the recipient
// list, message body, sender ID, and a typed ProviderOptions map for any
// provider-specific extras; the response is SMSResponse with a typed
// ProviderData field.
//
// Delivery reports are handled separately. Africa's Talking POSTs them as
// form-encoded requests; DeliveryReport is the parsed shape and
// DeliveryReportHandler dispatches each report to a callback (or, by default,
// to a structured slog entry). Build the handler with NewDeliveryReportHandler
// and override the default sink with WithReportHandler. Phone numbers are
// redacted before they reach logs.
//
// Provider implementations live under providers/. Today that is
// providers/africastalking, the Africa's Talking HTTP adapter.
package sms
