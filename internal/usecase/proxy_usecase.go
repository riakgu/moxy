package usecase

import (
	"fmt"
	"io"

	"github.com/sirupsen/logrus"

	"github.com/riakgu/moxy/internal/entity"
	"github.com/riakgu/moxy/internal/gateway/netns"
	"github.com/riakgu/moxy/internal/model"
)

type ProxyUseCase struct {
	Log      *logrus.Logger
	SlotUC   *SlotUseCase
	Dialer   *netns.Dialer
	Username string
	Password string
}

func NewProxyUseCase(log *logrus.Logger, slotUC *SlotUseCase, dialer *netns.Dialer, username, password string) *ProxyUseCase {
	return &ProxyUseCase{
		Log:      log,
		SlotUC:   slotUC,
		Dialer:   dialer,
		Username: username,
		Password: password,
	}
}

func (c *ProxyUseCase) Authenticate(req model.ProxyAuthRequest) (*entity.Slot, error) {
	if req.Password != c.Password {
		return nil, model.ErrInvalidCredentials
	}

	if req.Username != c.Username {
		return nil, model.ErrInvalidCredentials
	}

	if req.SlotName != "" {
		slot, err := c.SlotUC.SelectByName(req.SlotName)
		if err != nil {
			if c.Log != nil {
				c.Log.Warnf("sticky session failed for %s: %v", req.SlotName, err)
			}
			return nil, fmt.Errorf("%w: %s", model.ErrSlotNotFound, req.SlotName)
		}
		return slot, nil
	}

	slot, err := c.SlotUC.SelectRandom()
	if err != nil {
		return nil, model.ErrNoSlotsAvailable
	}
	return slot, nil
}

func (c *ProxyUseCase) Connect(slot *entity.Slot, targetAddr string) (io.ReadWriteCloser, error) {
	c.SlotUC.IncrementConnections(slot.Name)

	conn, err := c.Dialer.Dial(slot.Name, targetAddr)
	if err != nil {
		c.SlotUC.DecrementConnections(slot.Name)
		return nil, fmt.Errorf("dial %s via %s: %w", targetAddr, slot.Name, err)
	}

	return &trackedConn{
		ReadWriteCloser: conn,
		slotName:        slot.Name,
		slotUC:          c.SlotUC,
	}, nil
}

type trackedConn struct {
	io.ReadWriteCloser
	slotName string
	slotUC   *SlotUseCase
	closed   bool
}

func (tc *trackedConn) Close() error {
	if !tc.closed {
		tc.closed = true
		tc.slotUC.DecrementConnections(tc.slotName)
	}
	return tc.ReadWriteCloser.Close()
}
