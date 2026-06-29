// Package utils holds small, standalone helpers shared across the platform.
// Nothing here depends on the services above it; each file is a self-contained
// toolkit.
//
// # Money
//
// money.go does exact integer money arithmetic. Amounts are carried as int64 in
// a currency's smallest unit (cents, stroops) and computed through a
// decimal-backed library so there is no floating-point drift. It covers add,
// subtract, multiply, compare, and sign checks, plus parsing and formatting, and
// the financial routines built on them — simple and compound interest and
// currency conversion. The BasisPoints type expresses rates and fees as
// hundredths of a percent, with conversions to and from percentages and decimals.
//
// # Pagination
//
// pagination.go defines the shared Pagination input and the generic
// PaginatedResponse[T] envelope that list endpoints return.
//
// # PDF
//
// pdf_parser.go wraps PDF handling behind PDFParser: decrypting a
// password-protected document, extracting its text, and the combined
// parse-with-password convenience.
package utils
