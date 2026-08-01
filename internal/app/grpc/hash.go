package grpc

import (
	"crypto/md5"  //nolint:gosec // md5 hashing is part of the protocol contract
	"crypto/sha1" //nolint:gosec // sha1 hashing is part of the protocol contract
	"crypto/sha256"
	"crypto/sha512"
	"hash"
	"hash/crc32"
	"hash/crc64"

	pb "github.com/gameap/gameap/pkg/proto"
	"github.com/pkg/errors"
)

func hasherForAlgorithm(a pb.HashAlgorithm) (hash.Hash, error) {
	switch a {
	case pb.HashAlgorithm_HASH_ALGORITHM_MD5:
		return md5.New(), nil //nolint:gosec // md5 hashing is part of the protocol contract
	case pb.HashAlgorithm_HASH_ALGORITHM_SHA1:
		return sha1.New(), nil //nolint:gosec // sha1 hashing is part of the protocol contract
	case pb.HashAlgorithm_HASH_ALGORITHM_SHA256:
		return sha256.New(), nil
	case pb.HashAlgorithm_HASH_ALGORITHM_SHA512:
		return sha512.New(), nil
	case pb.HashAlgorithm_HASH_ALGORITHM_CRC32:
		return crc32.NewIEEE(), nil
	case pb.HashAlgorithm_HASH_ALGORITHM_CRC64:
		return crc64.New(crc64.MakeTable(crc64.ECMA)), nil
	default:
		return nil, errors.Errorf("unsupported hash algorithm: %s", a)
	}
}
