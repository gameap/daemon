package files

import (
	"io/fs"
	"os"
	"syscall"

	"github.com/et-nik/binngo"
	"github.com/gabriel-vasile/mimetype"
)

type fileType uint8

const (
	typeUnknown     fileType = 0
	typeDir         fileType = 1
	typeFile        fileType = 2
	typeCharDevice  fileType = 3
	typeBlockDevice fileType = 4
	typeNamedPipe   fileType = 5
	typeSymlink     fileType = 6
	typeSocket      fileType = 7
)

func fileTypeByMode(fileMode fs.FileMode) fileType {
	fType := typeUnknown

	switch {
	case fileMode&fs.ModeSymlink != 0:
		fType = typeSymlink
	case fileMode.IsRegular():
		fType = typeFile
	case fileMode.IsDir():
		fType = typeDir
	case fileMode&fs.ModeCharDevice != 0:
		fType = typeCharDevice
	case fileMode&fs.ModeDevice != 0:
		fType = typeBlockDevice
	case fileMode&fs.ModeNamedPipe != 0:
		fType = typeNamedPipe
	case fileMode&fs.ModeSocket != 0:
		fType = typeSocket
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

func createFileInfoResponse(fi fs.FileInfo) *fileInfoResponse {
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

	stat := fi.Sys().(*syscall.Stat_t)

	var mime string
	if fType == typeFile && stat.Size > 0 {
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
		AccessTime:       uint64(stat.Atim.Sec),
		CreateTime:       uint64(stat.Ctim.Sec),
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
