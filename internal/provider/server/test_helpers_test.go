package server

import (
	"encoding/json"
	"net/http"
	"testing"

	"github.com/viettelcloud-oss/sdks/go/core"
	serversdk "github.com/viettelcloud-oss/sdks/go/server"
)

var (
	placementGroupTestUUID    = core.UUID{0x71}
	placementGroupTestRegion  = core.UUID{0x72}
	placementGroupProjectID   = core.UUID{4}
	placementGroupTestProject = serversdk.NestedProjectSchema{
		Id:   placementGroupProjectID,
		Name: "integration",
		Slug: "integration",
	}
)

func writePlacementGroupResponse(t *testing.T, w http.ResponseWriter, pg *serversdk.PlacementGroupSchema) {
	t.Helper()
	if err := json.NewEncoder(w).Encode(pg); err != nil {
		t.Errorf("encode Placement Group response: %v", err)
	}
}
