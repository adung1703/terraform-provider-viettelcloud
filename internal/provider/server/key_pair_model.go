package server

import (
	"strings"
	"time"

	"github.com/hashicorp/terraform-plugin-framework/types"
	serversdk "github.com/viettelcloud-oss/sdks/go/server"
)

type KeyPairModel struct {
	ID          types.String `tfsdk:"id"`
	Name        types.String `tfsdk:"name"`
	PublicKey   types.String `tfsdk:"public_key"`
	Fingerprint types.String `tfsdk:"fingerprint"`
	Type        types.String `tfsdk:"type"`
	CreatedAt   types.String `tfsdk:"created_at"`
	UpdatedAt   types.String `tfsdk:"updated_at"`
}

type KeyPairResourceModel struct {
	KeyPairModel
	PrivateKey types.String `tfsdk:"private_key"`
}

type KeyPairDataSourceModel struct {
	KeyPairModel
}

func populateKeyPairModel(kp *serversdk.KeyPairSchema, m *KeyPairModel) {
	if kp == nil {
		return
	}

	m.ID = types.StringValue(kp.Id.String())
	m.Name = types.StringValue(kp.Name)
	m.PublicKey = types.StringValue(kp.PublicKey)
	m.Fingerprint = types.StringValue(kp.Fingerprint)
	m.Type = types.StringValue(string(kp.Type))
	m.CreatedAt = types.StringValue(kp.CreatedAt.Format(time.RFC3339))
	m.UpdatedAt = types.StringValue(kp.UpdatedAt.Format(time.RFC3339))
}

func populateKeyPairResourceState(kp *serversdk.KeyPairSchema, state *KeyPairResourceModel) {
	if kp == nil {
		return
	}

	configuredName := state.Name
	configuredPublicKey := state.PublicKey
	priorPrivateKey := state.PrivateKey
	populateKeyPairModel(kp, &state.KeyPairModel)
	state.PrivateKey = priorPrivateKey

	if !configuredName.IsNull() && !configuredName.IsUnknown() && strings.TrimSpace(configuredName.ValueString()) == kp.Name {
		state.Name = configuredName
	}
	if !configuredPublicKey.IsNull() && !configuredPublicKey.IsUnknown() && strings.TrimSpace(configuredPublicKey.ValueString()) == strings.TrimSpace(kp.PublicKey) {
		state.PublicKey = configuredPublicKey
	}
}

func populateKeyPairCreatedResourceState(created *serversdk.KeyPairCreatedSchema, state *KeyPairResourceModel) {
	if created == nil {
		return
	}

	configuredName := state.Name
	configuredPublicKey := state.PublicKey
	state.ID = types.StringValue(created.Id.String())
	state.Name = types.StringValue(created.Name)
	state.PublicKey = types.StringValue(created.PublicKey)
	state.Fingerprint = types.StringValue(created.Fingerprint)
	state.Type = types.StringValue(string(created.Type))
	state.CreatedAt = types.StringValue(created.CreatedAt.Format(time.RFC3339))
	state.UpdatedAt = types.StringValue(created.UpdatedAt.Format(time.RFC3339))

	if created.PrivateKey != "" {
		state.PrivateKey = types.StringValue(created.PrivateKey)
	} else {
		state.PrivateKey = types.StringNull()
	}

	if !configuredName.IsNull() && !configuredName.IsUnknown() && strings.TrimSpace(configuredName.ValueString()) == created.Name {
		state.Name = configuredName
	}
	if !configuredPublicKey.IsNull() && !configuredPublicKey.IsUnknown() && strings.TrimSpace(configuredPublicKey.ValueString()) == strings.TrimSpace(created.PublicKey) {
		state.PublicKey = configuredPublicKey
	}
}

func populateKeyPairDataSourceState(kp *serversdk.KeyPairSchema, state *KeyPairDataSourceModel) {
	populateKeyPairModel(kp, &state.KeyPairModel)
}
