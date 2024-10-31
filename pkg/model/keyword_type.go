package model

type KeywordType int

const (
	PROJECT KeywordType = iota
	ALARM
)

func (k KeywordType) String() string {
	names := [...]string{"PROJECT", "ALARM"}
	if k < PROJECT || k > ALARM {
		return "Unknown"
	}
	return names[k]
}
