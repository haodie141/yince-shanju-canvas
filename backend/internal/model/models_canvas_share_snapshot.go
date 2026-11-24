package model

import "time"

// CanvasShareSnapshot is independent of the bounded canvas edit history.
// A published version must keep its resource references until no share or
// active clone operation depends on it.
type CanvasShareSnapshot struct {
	ID             string    `gorm:"primaryKey;size:36"`
	ShareID        string    `gorm:"size:36;not null;index"`
	UserID         string    `gorm:"size:36;not null;index"`
	CanvasID       string    `gorm:"size:80;not null;index"`
	SourceRevision int64     `gorm:"not null"`
	Title          string    `gorm:"size:240"`
	PayloadJSON    string    `gorm:"type:text;not null"`
	CreatedAt      time.Time `gorm:"not null"`
}

type CanvasShareSnapshotResource struct {
	SnapshotID string `gorm:"primaryKey;size:36"`
	ResourceID string `gorm:"primaryKey;size:36;index"`
	// ResourceJSON and StorageSettingID are private, immutable storage metadata.
	// They are never returned as part of the public share response.
	ResourceJSON     string `gorm:"type:text;not null"`
	StorageSettingID string `gorm:"size:36"`
	Provider         string `gorm:"size:24"`
	Endpoint         string
	Bucket           string               `gorm:"size:160"`
	ObjectKey        string               `gorm:"index"`
	Snapshot         *CanvasShareSnapshot `gorm:"foreignKey:SnapshotID;constraint:OnDelete:RESTRICT"`
}

// CanvasShareCloneReceipt makes retries return the original recipient canvas.
type CanvasShareCloneReceipt struct {
	UserID     string    `gorm:"primaryKey;size:36"`
	RequestID  string    `gorm:"primaryKey;size:80"`
	ShareID    string    `gorm:"size:36;not null"`
	SnapshotID string    `gorm:"size:36;not null"`
	CanvasID   string    `gorm:"size:80;not null"`
	CreatedAt  time.Time `gorm:"not null"`
}
