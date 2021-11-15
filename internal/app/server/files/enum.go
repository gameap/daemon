package files

type Operation uint8

const (
	FileSend   Operation = 3
	ReadDir    Operation = 4
	MakeDir    Operation = 5
	FileMove   Operation = 6
	FileRemove Operation = 7
	FileInfo   Operation = 8
	FileChmod  Operation = 9
)

const (
	ListWithoutDetails = 0
	ListWithDetails    = 1
)

const (
	GetFileFromClient = 1
	SendFileToClient  = 2
)
