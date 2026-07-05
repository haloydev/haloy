package api

import "time"

const (
	defaultContextTimeout = 120 * time.Second

	// imageLoadTimeout bounds docker load of uploaded/assembled image tars,
	// which can take several minutes for large images on slow disks.
	imageLoadTimeout = 10 * time.Minute

	// maxLayerUploadBytes caps a single layer blob upload. Disk preflight handles
	// well-behaved clients; this bounds chunked bodies with no Content-Length.
	maxLayerUploadBytes = 16 << 30 // 16 GiB

	// maxJSONBodyBytes caps JSON request bodies on image endpoints. The largest
	// legitimate payload is an image config in an assemble request.
	maxJSONBodyBytes = 32 << 20 // 32 MiB
)
