package model

import "time"

// FeaturedCanvas is an administrator-curated video and a fixed canvas version.
// User-submitted community posts are intentionally a separate future feature.
type FeaturedCanvas struct {
	ID              string `json:"id" gorm:"primaryKey;size:36"`
	Title           string `json:"title" gorm:"size:160;not null"`
	Description     string `json:"description" gorm:"size:500"`
	Category        string `json:"category" gorm:"size:80"`
	VideoResourceID string `json:"-" gorm:"size:36;not null;index"`
	// VideoSource 记录展示视频的来源：upload 表示管理员上传，share 表示从
	// 绑定的分享快照视频节点选取；旧记录为空串，保存时按原视频复用。
	VideoSource string `json:"-" gorm:"size:16;not null;default:''"`
	// CoverResourceID 是首页卡片的视频封面；为空时显示视频首帧。
	// 封面可来自管理员上传的图片，或绑定分享快照内的图片节点。
	CoverResourceID string `json:"-" gorm:"size:36;index"`
	// CoverSource 记录封面来源：upload|share；空串表示没有自定义封面。
	CoverSource string `json:"-" gorm:"size:16;not null;default:''"`
	SnapshotID  string `json:"-" gorm:"size:36;not null;index"`
	// PersistCopy 表示管理员已选择持久保存平台副本：即使原画布被误删，
	// 首页展示、视频和克隆仍使用 CopySnapshotID 对应的独立快照与素材 pin。
	PersistCopy     bool                 `json:"-" gorm:"not null;default:false"`
	CopyStatus      string               `json:"-" gorm:"size:16;not null;default:'none'"`
	CopySnapshotID  string               `json:"-" gorm:"size:36;index"`
	CopySavedAt     *time.Time           `json:"-"`
	CopyError       string               `json:"-" gorm:"type:text"`
	CopyTokenHash   string               `json:"-" gorm:"size:64;index"`
	CopyTokenCipher string               `json:"-" gorm:"type:text"`
	ConsentAt       *time.Time           `json:"-"`
	Enabled         bool                 `json:"enabled" gorm:"index"`
	SortOrder       int                  `json:"sortOrder" gorm:"index"`
	CreatedBy       string               `json:"-" gorm:"size:36;not null;index"`
	CreatedAt       time.Time            `json:"createdAt"`
	UpdatedAt       time.Time            `json:"updatedAt"`
	Video           *Resource            `json:"-" gorm:"foreignKey:VideoResourceID;constraint:OnDelete:RESTRICT"`
	Snapshot        *CanvasShareSnapshot `json:"-" gorm:"foreignKey:SnapshotID;constraint:OnDelete:RESTRICT"`
}
