package providerdata

import (
	blockstoragesdk "github.com/viettelcloud-oss/sdks/go/blockstorage"
	"github.com/viettelcloud-oss/sdks/go/core"
	networksdk "github.com/viettelcloud-oss/sdks/go/network"
	projectsdk "github.com/viettelcloud-oss/sdks/go/project"
	serversdk "github.com/viettelcloud-oss/sdks/go/server"
)

type Configured struct {
	BlockStorage *blockstoragesdk.Client
	Network      *networksdk.Client
	Project      *projectsdk.Client
	Server       *serversdk.Client
	ProjectID    core.UUID
}
