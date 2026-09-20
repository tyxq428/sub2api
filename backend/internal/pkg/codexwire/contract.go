package codexwire

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"strings"
)

const GraphRevisionV1 = "r2.2-graph-v1"

type EvidencePlane string

const (
	EvidenceSemanticReady    EvidencePlane = "semantic_ready"
	EvidenceTransportReady   EvidencePlane = "transport_ready"
	EvidenceReceiverObserved EvidencePlane = "receiver_observed"
)

// Contract is a frozen, evidence-scoped description of one supported Codex
// wire family.  It intentionally contains no credentials, account ids or
// mutable runtime configuration.
type Contract struct {
	ID               string   `json:"id"`
	ProfileID        string   `json:"profile_id"`
	ReferenceTag     string   `json:"reference_tag"`
	ReferenceCommit  string   `json:"reference_commit"`
	ClientVersion    string   `json:"client_version"`
	GraphRevision    string   `json:"graph_revision"`
	Purposes         []string `json:"purposes"`
	EvidenceComplete bool     `json:"evidence_complete"`
}

var contracts = []Contract{
	{
		ID:               "codex-wire-0.154-r1",
		ProfileID:        "codex-0.154-profile-r1",
		ReferenceTag:     "rust-v0.154.0",
		ReferenceCommit:  "6b9826e3aa83b1a5947db50f4332cb9c65f1b340",
		ClientVersion:    "0.154.0",
		GraphRevision:    GraphRevisionV1,
		Purposes:         []string{"inference", "compact", "websocket"},
		EvidenceComplete: true,
	},
	{
		ID:               "codex-wire-0.155.1-r1",
		ProfileID:        "codex-0.155.1-profile-r1",
		ReferenceTag:     "rust-v0.155.1",
		ReferenceCommit:  "be2951ea34f0d295ed0becf97079f92fa5f6950e",
		ClientVersion:    "0.155.1",
		GraphRevision:    GraphRevisionV1,
		Purposes:         []string{"inference", "compact", "websocket"},
		EvidenceComplete: false,
	},
}

func KnownContracts() []Contract {
	out := make([]Contract, len(contracts))
	copy(out, contracts)
	for i := range out {
		out[i].Purposes = append([]string(nil), out[i].Purposes...)
	}
	return out
}

func ContractForProfile(profileID string) (Contract, bool) {
	profileID = strings.TrimSpace(profileID)
	for _, contract := range contracts {
		if contract.ProfileID == profileID {
			contract.Purposes = append([]string(nil), contract.Purposes...)
			return contract, true
		}
	}
	return Contract{}, false
}

func ContractByID(id string) (Contract, bool) {
	id = strings.TrimSpace(id)
	for _, contract := range contracts {
		if contract.ID == id {
			contract.Purposes = append([]string(nil), contract.Purposes...)
			return contract, true
		}
	}
	return Contract{}, false
}

func (c Contract) SupportsPurpose(purpose string) bool {
	purpose = strings.TrimSpace(purpose)
	for _, allowed := range c.Purposes {
		if allowed == purpose {
			return true
		}
	}
	return false
}

// Digest is the stable contract fingerprint persisted beside a new R2.2
// binding.  A changed contract must therefore produce a new digest even if an
// operator accidentally reuses an id.
func (c Contract) Digest() string {
	encoded, _ := json.Marshal(c)
	digest := sha256.Sum256(encoded)
	return hex.EncodeToString(digest[:])
}
