package files

type Operation uint8

const (
	FileSend   Operation = 3
	ReadDir              = 4
	MakeDir              = 5
	FileMove             = 6
	FileRemove           = 7
	FileInfo             = 8
	FileChmod            = 9
)

const (
	ListWithoutDetails = 0
	ListWithDetails    = 1
)

const (
	SendFileToClient  = 1
	GetFileFromClient = 2
)
