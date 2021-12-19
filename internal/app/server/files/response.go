package files

import (
	"os"

	"github.com/et-nik/binngo"
	"github.com/gabriel-vasile/mimetype"
)

func fileTypeByMode(fileMode os.FileMode) FileType {
	fType := TypeUnknown

	switch {
	case fileMode&os.ModeSymlink != 0:
		fType = TypeSymlink
	case fileMode.IsRegular():
		fType = TypeFile
	case fileMode.IsDir():
		fType = TypeDir
	case fileMode&os.ModeCharDevice != 0:
		fType = TypeCharDevice
	case fileMode&os.ModeDevice != 0:
		fType = TypeBlockDevice
	case fileMode&os.ModeNamedPipe != 0:
		fType = TypeNamedPipe
	case fileMode&os.ModeSocket != 0:
		fType = TypeSocket
	}

	return fType
}

type fileInfoResponse struct {
	Name         string
	Size         uint64
	TimeModified uint64
	Type         uint8
	Perm         uint32
}

func createFileInfoResponse(fi os.FileInfo) *fileInfoResponse {
	fType := fileTypeByMode(fi.Mode())

	return &fileInfoResponse{
		Name:         fi.Name(),
		Size:         uint64(fi.Size()),
		TimeModified: uint64(fi.ModTime().Unix()),
		Type:         uint8(fType),
		Perm:         uint32(fi.Mode().Perm()),
	}
}

func (fi fileInfoResponse) MarshalBINN() ([]byte, error) {
	resp := []interface{}{fi.Name, fi.Size, fi.TimeModified, fi.Type, fi.Perm}
	return binngo.Marshal(&resp)
}

//nolint:maligned
type fileDetailsResponse struct {
	Name             string
	Size             uint64
	Type             uint8
	ModificationTime uint64
	AccessTime       uint64
	CreateTime       uint64
	Perm             uint32
	Mime             string
}

func createfileDetailsResponse(path string) (*fileDetailsResponse, error) {
	fi, err := os.Lstat(path)
	if err != nil {
		return nil, err
	}
	fType := fileTypeByMode(fi.Mode())

	fileTime := fileTimeFromFileInfo(fi)

	var mime string
	if fType == TypeFile && fi.Size() > 0 {
		mm, err := mimetype.DetectFile(path)

		if err != nil {
			return nil, err
		}

		mime = mm.String()
	}

	return &fileDetailsResponse{
		Name:             fi.Name(),
		Size:             uint64(fi.Size()),
		Type:             uint8(fType),
		ModificationTime: uint64(fi.ModTime().Unix()),
		AccessTime:       fileTime.AccessTime,
		CreateTime:       fileTime.CreatingTime,
		Perm:             uint32(fi.Mode().Perm()),
		Mime:             mime,
	}, nil
}

func (fdr fileDetailsResponse) MarshalBINN() ([]byte, error) {
	resp := []interface{}{
		fdr.Name,
		fdr.Size,
		fdr.Type,
		fdr.ModificationTime,
		fdr.AccessTime,
		fdr.CreateTime,
		fdr.Perm,
		fdr.Mime,
	}
	return binngo.Marshal(&resp)
}
