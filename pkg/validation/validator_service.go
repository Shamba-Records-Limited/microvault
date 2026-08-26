package validation

import (
	"errors"
	"fmt"
	"strings"

	"github.com/go-playground/validator/v10"
)

// ErrorMessageFunc is a function that generates an error message for a validation error.
// It receives the field name and the validation parameter (if any).
type ErrorMessageFunc func(field string, param string) string

// ValidatorService wraps the validator.Validate instance and provides
// custom validation functionality with user-friendly error messages.
type ValidatorService struct {
	validate      *validator.Validate
	errorMessages map[string]ErrorMessageFunc
}

// NewValidatorService creates a new validation service with default validators.
func NewValidatorService() *ValidatorService {
	v := validator.New()
	service := &ValidatorService{
		validate:      v,
		errorMessages: make(map[string]ErrorMessageFunc),
	}

	// Register custom validators
	service.registerDefaultValidators()

	return service
}

// registerDefaultValidators registers all default custom validators.
func (vs *ValidatorService) registerDefaultValidators() {
	// Register stellar XDR validator with its error message
	vs.RegisterValidator("stellar_xdr", validateStellarXDR, func(field, param string) string {
		return fmt.Sprintf("%s must be a valid Stellar XDR transaction", field)
	})
}

// RegisterValidator registers a custom validator function with its error message.
// tag: the validation tag to use in struct tags (e.g., "stellar_xdr")
// validatorFn: the validation function
// errorMsgFn: the error message function (receives field name and param)
func (vs *ValidatorService) RegisterValidator(tag string, validatorFn validator.Func, errorMsgFn ErrorMessageFunc) error {
	// Register the validator
	if err := vs.validate.RegisterValidation(tag, validatorFn); err != nil {
		return err
	}

	// Register the error message
	vs.errorMessages[tag] = errorMsgFn

	return nil
}

// RegisterErrorMessage registers or updates an error message for a validation tag.
// This is useful when you want to customize error messages for built-in validators.
func (vs *ValidatorService) RegisterErrorMessage(tag string, errorMsgFn ErrorMessageFunc) {
	vs.errorMessages[tag] = errorMsgFn
}

// Validate validates a struct and returns field-specific error messages.
// Returns a map where keys are field names (in snake_case) and values are error messages.
func (vs *ValidatorService) Validate(data interface{}) (map[string]string, error) {
	err := vs.validate.Struct(data)
	if err == nil {
		return nil, nil
	}

	// Check if it's validation errors
	var validationErrors validator.ValidationErrors
	ok := errors.As(err, &validationErrors)
	if !ok {
		return nil, err
	}

	// Convert validation errors to field-specific messages
	fieldErrors := make(map[string]string)
	for _, fieldErr := range validationErrors {
		fieldName := toSnakeCase(fieldErr.Field())
		fieldErrors[fieldName] = vs.getErrorMessage(fieldErr)
	}

	return fieldErrors, nil
}

// getErrorMessage converts a validator.FieldError to a user-friendly message.
func (vs *ValidatorService) getErrorMessage(fe validator.FieldError) string {
	tag := fe.Tag()
	field := fe.Field()
	param := fe.Param()

	// Check if we have a custom error message for this tag
	if errorMsgFn, exists := vs.errorMessages[tag]; exists {
		return errorMsgFn(field, param)
	}

	// Fallback: Generate a reasonable error message based on the tag
	return vs.generateDefaultMessage(field, tag, param)
}

// generateDefaultMessage generates a sensible default error message for common validation tags.
func (vs *ValidatorService) generateDefaultMessage(field, tag, param string) string {
	switch tag {
	case "required":
		return fmt.Sprintf("%s is required", field)
	case "email":
		return fmt.Sprintf("%s must be a valid email address", field)
	case "min":
		return fmt.Sprintf("%s must be at least %s characters", field, param)
	case "max":
		return fmt.Sprintf("%s must be at most %s characters", field, param)
	case "len":
		return fmt.Sprintf("%s must be exactly %s characters", field, param)
	case "eq":
		return fmt.Sprintf("%s must be equal to %s", field, param)
	case "ne":
		return fmt.Sprintf("%s must not be equal to %s", field, param)
	case "gt":
		return fmt.Sprintf("%s must be greater than %s", field, param)
	case "gte":
		return fmt.Sprintf("%s must be greater than or equal to %s", field, param)
	case "lt":
		return fmt.Sprintf("%s must be less than %s", field, param)
	case "lte":
		return fmt.Sprintf("%s must be less than or equal to %s", field, param)
	case "alpha":
		return fmt.Sprintf("%s must contain only alphabetic characters", field)
	case "alphanum":
		return fmt.Sprintf("%s must contain only alphanumeric characters", field)
	case "numeric":
		return fmt.Sprintf("%s must be a valid numeric value", field)
	case "url":
		return fmt.Sprintf("%s must be a valid URL", field)
	case "uri":
		return fmt.Sprintf("%s must be a valid URI", field)
	case "base64":
		return fmt.Sprintf("%s must be valid base64 encoded data", field)
	case "uuid":
		return fmt.Sprintf("%s must be a valid UUID", field)
	case "uuid4":
		return fmt.Sprintf("%s must be a valid UUID v4", field)
	default:
		// Generic fallback for unknown tags
		if param != "" {
			return fmt.Sprintf("%s failed validation (%s: %s)", field, tag, param)
		}
		return fmt.Sprintf("%s failed validation (%s)", field, tag)
	}
}

// toSnakeCase converts a string from PascalCase/camelCase to snake_case.
// Handles acronyms properly
func toSnakeCase(s string) string {
	if s == "" {
		return ""
	}

	var result strings.Builder
	runes := []rune(s)

	for i := range len(runes) {
		r := runes[i]

		// Add underscore before uppercase letter if:
		// 1. It's not the first character
		// 2. The previous character was lowercase OR
		// 3. The next character exists and is lowercase (handles acronyms like "ID" in "UserID")
		if i > 0 && r >= 'A' && r <= 'Z' {
			prevIsLower := runes[i-1] >= 'a' && runes[i-1] <= 'z'
			nextIsLower := i+1 < len(runes) && runes[i+1] >= 'a' && runes[i+1] <= 'z'

			if prevIsLower || nextIsLower {
				result.WriteRune('_')
			}
		}

		result.WriteRune(r)
	}

	return strings.ToLower(result.String())
}
