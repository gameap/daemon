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

type FileType uint8

const (
	TypeUnknown     FileType = 0
	TypeDir         FileType = 1
	TypeFile        FileType = 2
	TypeCharDevice  FileType = 3
	TypeBlockDevice FileType = 4
	TypeNamedPipe   FileType = 5
	TypeSymlink     FileType = 6
	TypeSocket      FileType = 7
)
