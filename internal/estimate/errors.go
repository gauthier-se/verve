package estimate

import "errors"

// ErrInsufficientData is returned when no basis can produce an Expenditure estimate:
// no intake history, no recorded expenditure, and no Basal estimate to scale. It is a
// distinct error rather than a zero because zero is a *number*, and the whole point of
// this package is that a figure without provenance must never be presented as one.
var ErrInsufficientData = errors.New("estimate: not enough data for an expenditure estimate")
