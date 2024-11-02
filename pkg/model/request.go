package model

type RequestInfo struct {
	ChatId         int64
	MessageId      int
	ReplyMessageId int
	Message        string
	FileName       string
	Type           FileType
}

type FileType int

const (
	PDF FileType = iota
	IMG
)
