package entity

import (
	"encoding/json"
	"time"
)

// QRAction identifies what side effect a confirmed QR session performs.
// Each action interprets Params differently and produces a different confirm-time result.
type QRAction string

const (
	// QRActionLogin — a Web / desktop client requests a login; APP confirms and
	// receives a session token on the next status poll. No Params required.
	QRActionLogin QRAction = "login"

	// QRActionJoinOrg — an org admin generates a join code; a user confirms and
	// is added to organizations.members. Params: {"org_id": int64, "role": string}.
	QRActionJoinOrg QRAction = "join_org"

	// QRActionDelegate — grant a time-bound scoped access token to another
	// account. Params: {"scopes": []string, "ttl_seconds": int}.
	QRActionDelegate QRAction = "delegate"
)

// QRStatus is the lifecycle state of a QR session.
type QRStatus string

const (
	QRStatusPending   QRStatus = "pending"
	QRStatusConfirmed QRStatus = "confirmed"
	QRStatusConsumed  QRStatus = "consumed"
)

// QRSession is the in-Redis representation of an outstanding QR interaction.
//
// The struct is serialised as JSON and stored at key "qr:<id>" with a short
// TTL (default 5 min). All transitions go through Lua scripts so that
// pending→confirmed and confirmed→consumed are both atomic single-RTT ops.
type QRSession struct {
	ID        string          `json:"id"`
	Action    QRAction        `json:"action"`
	Params    json.RawMessage `json:"params,omitempty"`
	Status    QRStatus        `json:"status"`
	AccountID int64           `json:"account_id,omitempty"` // set when confirmed (scanner/APP user)
	// CreatedBy identifies the initiator of the session when create requires auth
	// (e.g. join_org / delegate). Zero for unauthenticated actions like login.
	// Required when the confirm-time side effect needs to act "on behalf of" the
	// initiator — e.g. for join_org it is the org admin who minted the code.
	CreatedBy int64     `json:"created_by,omitempty"`
	CreatedAt time.Time `json:"created_at"`
	IP        string    `json:"ip,omitempty"` // createSession remote IP, for audit
	UA        string    `json:"ua,omitempty"` // createSession UA, shown on Confirm UI
}

// IsValidQRAction reports whether an incoming action string matches a known kind.
func IsValidQRAction(a QRAction) bool {
	switch a {
	case QRActionLogin, QRActionJoinOrg, QRActionDelegate:
		return true
	}
	return false
}
