package app

import (
	"errors"
	"testing"

	"yingce/backend/internal/model"
	"yingce/backend/internal/repository"

	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

func newDefaultModelsTestService(t *testing.T) (*Service, *gorm.DB) {
	t.Helper()
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	if sqlDB, err := db.DB(); err == nil {
		sqlDB.SetMaxOpenConns(1)
	}
	if err := db.AutoMigrate(&model.SystemSetting{}, &model.AdminAuditEvent{}, &model.ModelChannel{}, &model.ChannelModel{}, &model.ChannelModelPriceTier{}); err != nil {
		t.Fatal(err)
	}
	return New(repository.New(db), t.TempDir()), db
}

func seedDefaultModelChannel(t *testing.T, db *gorm.DB, channelID string, models []model.ChannelModel) {
	t.Helper()
	channel := &model.ModelChannel{ID: channelID, Scope: model.ChannelScopeSystem, Enabled: true, Name: channelID}
	if err := db.Create(channel).Error; err != nil {
		t.Fatal(err)
	}
	for index := range models {
		models[index].ChannelID = channelID
		if models[index].ID == "" {
			models[index].ID = channelID + "-" + models[index].ModelKey
		}
		if err := db.Create(&models[index]).Error; err != nil {
			t.Fatal(err)
		}
	}
}

func TestDefaultModelsEmptyUntilConfigured(t *testing.T) {
	svc, _ := newDefaultModelsTestService(t)
	setting, err := svc.DefaultModels()
	if err != nil {
		t.Fatal(err)
	}
	if setting.Configured || setting.ImageModel != "" || setting.AgentTextModel != "" {
		t.Fatalf("DefaultModels() = %#v", setting)
	}
}

func TestUpdateDefaultModelsRejectsInvalidReferenceAndCapability(t *testing.T) {
	svc, db := newDefaultModelsTestService(t)
	admin := &model.User{ID: "admin-1", Role: model.UserRoleAdmin}
	seedDefaultModelChannel(t, db, "ch-1", []model.ChannelModel{
		{ModelKey: "img-1", Capability: "image", Enabled: true},
		{ModelKey: "txt-1", Capability: "text", Enabled: true},
	})

	if _, err := svc.UpdateDefaultModels(&model.User{ID: "user-1", Role: model.UserRoleUser}, DefaultModels{}); err == nil {
		t.Fatal("non-admin update must fail")
	}

	for _, value := range []DefaultModels{
		{ImageModel: "missing::img-1"},
		{ImageModel: "ch-1::missing"},
		{VideoModel: "ch-1::img-1"},
	} {
		_, err := svc.UpdateDefaultModels(admin, value)
		var authErr *AuthError
		if !errors.As(err, &authErr) || authErr.Status != 400 {
			t.Fatalf("UpdateDefaultModels(%#v) error = %#v", value, err)
		}
	}
}

func TestUpdateDefaultModelsPersistsAndExposesToUsers(t *testing.T) {
	svc, db := newDefaultModelsTestService(t)
	admin := &model.User{ID: "admin-1", Role: model.UserRoleAdmin}
	seedDefaultModelChannel(t, db, "ch-1", []model.ChannelModel{
		{ModelKey: "img-1", Capability: "image", Enabled: true},
		{ModelKey: "vid-1", Capability: "video", Enabled: true},
		{ModelKey: "txt-1", Capability: "text", Enabled: true},
		{ModelKey: "aud-1", Capability: "audio", Enabled: true},
	})

	want := DefaultModels{
		ImageModel:     "ch-1::img-1",
		VideoModel:     "ch-1::vid-1",
		AgentTextModel: "ch-1::txt-1",
		OptimizerModel: "ch-1::txt-1",
		AudioModel:     "ch-1::aud-1",
	}
	setting, err := svc.UpdateDefaultModels(admin, want)
	if err != nil {
		t.Fatal(err)
	}
	if !setting.Configured || setting.ImageModel != want.ImageModel || setting.AgentTextModel != want.AgentTextModel || setting.OptimizerModel != want.OptimizerModel {
		t.Fatalf("UpdateDefaultModels() = %#v", setting)
	}

	userSetting, err := svc.DefaultModels()
	if err != nil {
		t.Fatal(err)
	}
	if userSetting.ImageModel != want.ImageModel || userSetting.VideoModel != want.VideoModel || userSetting.AudioModel != want.AudioModel {
		t.Fatalf("DefaultModels() = %#v", userSetting)
	}

	var auditCount int64
	if err := db.Model(&model.AdminAuditEvent{}).Where("action = ?", "default_models.update").Count(&auditCount).Error; err != nil {
		t.Fatal(err)
	}
	if auditCount != 1 {
		t.Fatalf("audit count = %d, want 1", auditCount)
	}
}
