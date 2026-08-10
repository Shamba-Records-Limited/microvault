// Package controllers is the HTTP handler layer, built on Fiber. Each controller
// is a thin adapter for one group of routes: it parses and validates the incoming
// request, calls a service, and maps the result or error onto an HTTP response.
// Business logic stays in the services below; controllers only translate between
// HTTP and those services.
//
// Every controller is constructed with its dependencies injected (NewAuthController,
// NewWebhookController, and so on), so handlers hold interfaces rather than
// reaching for globals. The doc comments carry Swagger annotations, which drive
// the generated API reference.
//
// # The controllers
//
// AuthController serves the challenge-response endpoints: it issues a Stellar
// challenge and, on a valid signed response, returns a JWT (see the auth package).
//
// WebhookController receives inbound payment-provider webhooks. It verifies the
// provider's HMAC signature against the configured secret before handing the
// event to the webhook layer, and rejects anything unsigned or mismatched.
//
// USSDController and SMSCallbackController handle the mobile-gateway callbacks.
// The USSD handler takes the provider from the URL path, forwards the session to
// the USSD service, and returns the CON/END string the gateway expects; the SMS
// handler ingests delivery-report callbacks and passes them to the SMS layer.
package controllers
