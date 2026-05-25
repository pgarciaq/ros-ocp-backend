package api

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
)

// ContainerCursor is the decoded keyset position for container list pagination.
type ContainerCursor struct {
	Namespace     string `json:"ns"`
	Workload      string `json:"wl"`
	ContainerName string `json:"cn"`
}

// NamespaceCursor is the decoded keyset position for namespace list pagination.
type NamespaceCursor struct {
	Namespace   string `json:"ns"`
	ClusterUUID string `json:"cu"`
}

// EncodeContainerCursor returns an opaque base64url cursor for the next page.
func EncodeContainerCursor(c ContainerCursor) string {
	b, _ := json.Marshal(c)
	return base64.RawURLEncoding.EncodeToString(b)
}

// DecodeContainerCursor decodes an opaque container list cursor.
func DecodeContainerCursor(s string) (ContainerCursor, error) {
	b, err := base64.RawURLEncoding.DecodeString(s)
	if err != nil {
		return ContainerCursor{}, fmt.Errorf("invalid after cursor: %w", err)
	}
	var c ContainerCursor
	if err := json.Unmarshal(b, &c); err != nil {
		return ContainerCursor{}, fmt.Errorf("invalid after cursor: %w", err)
	}
	return c, nil
}

// EncodeNamespaceCursor returns an opaque base64url cursor for the next namespace page.
func EncodeNamespaceCursor(c NamespaceCursor) string {
	b, _ := json.Marshal(c)
	return base64.RawURLEncoding.EncodeToString(b)
}

// DecodeNamespaceCursor decodes an opaque namespace list cursor.
func DecodeNamespaceCursor(s string) (NamespaceCursor, error) {
	b, err := base64.RawURLEncoding.DecodeString(s)
	if err != nil {
		return NamespaceCursor{}, fmt.Errorf("invalid after cursor: %w", err)
	}
	var c NamespaceCursor
	if err := json.Unmarshal(b, &c); err != nil {
		return NamespaceCursor{}, fmt.Errorf("invalid after cursor: %w", err)
	}
	return c, nil
}
