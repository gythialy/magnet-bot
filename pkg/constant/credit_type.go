package constant

// CreditType represents the type of credit record
type CreditType string

const (
	// CreditTypeBreakFaith represents a break of faith record
	CreditTypeBreakFaith CreditType = "breakFaith"
	// CreditTypeSuspend represents a suspension record
	CreditTypeSuspend CreditType = "suspend"
)

// String returns the string representation of CreditType
func (ct CreditType) String() string {
	return string(ct)
}

// IsValid checks if the CreditType is valid
func (ct CreditType) IsValid() bool {
	switch ct {
	case CreditTypeBreakFaith, CreditTypeSuspend:
		return true
	default:
		return false
	}
}
