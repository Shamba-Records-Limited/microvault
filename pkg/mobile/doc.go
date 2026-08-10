// Package mobile is the umbrella for the platform's mobile-channel surface.
// It is an organizational namespace with no Go code of its own; the actual
// behaviour lives in its sub-packages.
//
// sms holds the outbound SMS service (provider registry, send, and delivery
// report callbacks). ussd holds the interactive USSD application — session
// state, menu engine, localization, and the adapters that bridge the USSD
// flow to the user, account, loan, off-ramp, and treasury services.
package mobile
