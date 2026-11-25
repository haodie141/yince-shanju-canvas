package app

import (
	"fmt"
	"strings"
	"testing"

	"yingce/backend/internal/model"
	"yingce/backend/internal/production"
)

func TestDramaReferenceFrameReservesImageSlot(t *testing.T) {
	svc, fake := newDramaTestService(t)
	video := dramaVideoFixture()
	video.References.MaxImages = 9
	setDramaVideoCapability(t, svc, fake, video)
	settings := DramaSettings{Settings: production.Settings{AspectRatio: "9:16"}, Models: fakeDramaModels, VideoContinuity: DramaContinuityReference}
	previous := model.DramaSegment{VideoStatus: model.DramaMediaReady, TailFrameResourceID: fake.resource("usr-drama", "image", "image/png")}
	draft := dramaSegmentDraft{}
	draft.view.DurationMs = 5000
	casts := map[string]model.DramaCast{}
	for i := 1; i <= 9; i++ {
		id := fmt.Sprintf("cast-%d", i)
		casts[id] = model.DramaCast{ID: id, Name: id, ImageStatus: model.DramaMediaReady, ImageResourceID: fake.resource("usr-drama", "image", "image/png")}
		draft.view.Request.References = append(draft.view.Request.References, DramaSegmentReference{CastID: id, Name: id, Label: fmt.Sprintf("参考图%d", i)})
	}
	request, refs, _, err := svc.prepareDramaSegment(settings, draft, model.DramaSegment{}, &previous, casts)
	if err != nil {
		t.Fatal(err)
	}
	if request.ReferenceCapacity != 8 || len(refs) != 9 || request.References[8].Sent {
		t.Fatalf("must send eight images and one continuity image: %+v refs=%d", request, len(refs))
	}
	if ref := refs[8].(map[string]any); ref["role"] != nil || ref["storageKey"] != "resource:"+previous.TailFrameResourceID {
		t.Fatalf("continuity must be a normal reference: %+v", ref)
	}
	if !strings.Contains(request.Header, "参考图9是上一段视频的最后一帧") || dramaSentReferenceText("场景（参考图9）", request.References) != "场景（未附图）" {
		t.Fatalf("omitted reference must not point to continuity image: %s", request.Header)
	}
}

func TestDramaSentReferenceTextHandlesMissingMiddleImage(t *testing.T) {
	refs := []DramaSegmentReference{{Sent: true}, {Sent: false}, {Sent: true}}
	got := dramaSentReferenceText("甲（参考图1）乙（@参考图2）丙（参考图 3）", refs)
	if got != "甲（参考图1）乙（未附图）丙（参考图2）" {
		t.Fatalf("incorrect mapping: %s", got)
	}
}
