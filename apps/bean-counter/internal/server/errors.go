package server

import (
	"errors"

	"github.com/gofiber/fiber/v3"

	appstore "github.com/mattsp1290/bean-counter/internal/store"
)

var ErrValidation = errors.New("validation failed")

type FieldError struct {
	Field   string `json:"field"`
	Message string `json:"message"`
}

type ValidationError struct {
	Message string       `json:"message"`
	Fields  []FieldError `json:"fields,omitempty"`
}

func (e ValidationError) Error() string {
	if e.Message != "" {
		return e.Message
	}
	return ErrValidation.Error()
}

func (e ValidationError) Unwrap() error {
	return ErrValidation
}

type errorResponse struct {
	Error   string       `json:"error"`
	Message string       `json:"message"`
	Fields  []FieldError `json:"fields,omitempty"`
}

func ErrorHandler(c fiber.Ctx, err error) error {
	status, code, message, fields := classifyError(err)
	return c.Status(status).JSON(errorResponse{
		Error:   code,
		Message: message,
		Fields:  fields,
	})
}

func classifyError(err error) (int, string, string, []FieldError) {
	if err == nil {
		return fiber.StatusInternalServerError, "internal_error", "internal server error", nil
	}

	var validation *ValidationError
	if errors.As(err, &validation) {
		return fiber.StatusBadRequest, "validation_error", validation.Error(), validation.Fields
	}
	var validationValue ValidationError
	if errors.As(err, &validationValue) {
		return fiber.StatusBadRequest, "validation_error", validationValue.Error(), validationValue.Fields
	}
	if errors.Is(err, ErrValidation) {
		return fiber.StatusBadRequest, "validation_error", err.Error(), nil
	}
	if errors.Is(err, appstore.ErrNotFound) {
		return fiber.StatusNotFound, "not_found", err.Error(), nil
	}
	if errors.Is(err, appstore.ErrCycle) ||
		errors.Is(err, appstore.ErrDuplicateDep) ||
		errors.Is(err, appstore.ErrConflict) {
		return fiber.StatusConflict, "conflict", err.Error(), nil
	}
	if errors.Is(err, appstore.ErrEmptyDSN) ||
		errors.Is(err, appstore.ErrUnsupportedDriver) {
		return fiber.StatusInternalServerError, "store_configuration_error", err.Error(), nil
	}

	var fiberErr *fiber.Error
	if errors.As(err, &fiberErr) {
		return fiberErr.Code, errorCodeForStatus(fiberErr.Code), fiberErr.Message, nil
	}

	return fiber.StatusInternalServerError, "internal_error", "internal server error", nil
}

func errorCodeForStatus(status int) string {
	switch status {
	case fiber.StatusBadRequest:
		return "bad_request"
	case fiber.StatusNotFound:
		return "not_found"
	case fiber.StatusConflict:
		return "conflict"
	default:
		if status >= 500 {
			return "internal_error"
		}
		return "request_error"
	}
}
