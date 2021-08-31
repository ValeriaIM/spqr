package internal

import (
	"github.com/jackc/pgproto3"
	"github.com/pg-sharding/spqr/internal/config"
	"github.com/pkg/errors"
)

type RelayState struct {
	TxActive bool

	ActiveBackendConn Server
	ActiveShard       Shard
}

type ConnManager interface {
	TXBeginCB(client *SpqrClient, rst *RelayState) error
	TXEndCB(client *SpqrClient, rst *RelayState) error

	RouteCB(client *SpqrClient, rst *RelayState) error
	UnRouteCB(client *SpqrClient, rst *RelayState) error
	UnRouteWithError(client *SpqrClient, rst *RelayState, err error) error
	ValidateReRoute(rst *RelayState) bool
}

func unRouteWithError(cmngr ConnManager, client *SpqrClient, rst *RelayState, err error) error {
	_ = cmngr.UnRouteCB(client, rst)

	return client.Send(&pgproto3.ErrorResponse{
		Message:  err.Error(),
		Severity: "FATAL",
	})
}

type TxConnManager struct {
}

func (t *TxConnManager) UnRouteWithError(client *SpqrClient, rst *RelayState, err error) error {
	return unRouteWithError(t, client, rst, err)
}

func (t *TxConnManager) UnRouteCB(cl *SpqrClient, rst *RelayState) error {
	if rst.ActiveShard != nil {
		return cl.Route().Unroute(rst.ActiveShard.Name(), cl)
	}
	return nil
}

func NewTxConnManager() *TxConnManager {
	return &TxConnManager{}
}

func (t *TxConnManager) RouteCB(client *SpqrClient, rst *RelayState) error {

	shConn, err := client.Route().GetConn("tcp6", rst.ActiveShard)

	if err != nil {
		return err
	}

	client.AssignServerConn(shConn)

	return nil
}

func (t *TxConnManager) ValidateReRoute(rst *RelayState) bool {
	return rst.ActiveShard == nil || !rst.TxActive
}

func (t *TxConnManager) TXBeginCB(client *SpqrClient, rst *RelayState) error {
	return nil
}

func (t *TxConnManager) TXEndCB(client *SpqrClient, rst *RelayState) error {

	_ = client.Route().Unroute(rst.ActiveShard.Name(), client)
	rst.ActiveShard = nil

	return nil
}

type SessConnManager struct {
}

func (s *SessConnManager) UnRouteWithError(client *SpqrClient, rst *RelayState, err error) error {
	return unRouteWithError(s, client, rst, err)
}

func (s *SessConnManager) UnRouteCB(cl *SpqrClient, rst *RelayState) error {
	if rst.ActiveShard != nil {
		return cl.Route().Unroute(rst.ActiveShard.Name(), cl)
	}
	return nil
}

func (s *SessConnManager) TXBeginCB(client *SpqrClient, rst *RelayState) error {
	return nil
}

func (s *SessConnManager) TXEndCB(client *SpqrClient, rst *RelayState) error {
	return nil
}

func (s *SessConnManager) RouteCB(client *SpqrClient, rst *RelayState) error {

	shConn, err := client.Route().GetConn("tcp6", rst.ActiveShard)

	if err != nil {
		return err
	}

	client.AssignServerConn(shConn)
	rst.ActiveBackendConn = shConn

	return nil
}

func (s *SessConnManager) ValidateReRoute(rst *RelayState) bool {
	return rst.ActiveShard == nil
}

func NewSessConnManager() *SessConnManager {
	return &SessConnManager{}
}

func InitClConnection(client *SpqrClient) (ConnManager, error) {

	var connmanager ConnManager
	switch client.Rule.PoolingMode {
	case config.PoolingModeSession:
		connmanager = NewSessConnManager()
	case config.PoolingModeTransaction:
		connmanager = NewTxConnManager()
	default:
		for _, msg := range []pgproto3.BackendMessage{
			&pgproto3.ErrorResponse{
				Message:  "unknown pooling mode for route",
				Severity: "ERROR",
			},
		} {
			if err := client.Send(msg); err != nil {
				return nil, err
			}
		}
		return nil, errors.Errorf("unknown pooling mode %v", client.Rule.PoolingMode)
	}

	return connmanager, nil
}
