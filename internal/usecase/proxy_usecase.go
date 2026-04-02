package usecase

import (
	"database/sql"
	"fmt"
	"net"

	"github.com/sirupsen/logrus"

	"github.com/riakgu/moxy/internal/model"
	"github.com/riakgu/moxy/internal/model/converter"
	"github.com/riakgu/moxy/internal/repository"
)

type SlotDialer interface {
	Dial(slotName string, addr string) (net.Conn, error)
}

type ProxyUseCase struct {
	Log      *logrus.Logger
	SlotUC   *SlotUseCase
	Dialer   SlotDialer
	UserRepo *repository.ProxyUserRepository
	DB       *sql.DB
}

func NewProxyUseCase(log *logrus.Logger, slotUC *SlotUseCase, dialer SlotDialer, userRepo *repository.ProxyUserRepository, db *sql.DB) *ProxyUseCase {
	return &ProxyUseCase{
		Log:      log,
		SlotUC:   slotUC,
		Dialer:   dialer,
		UserRepo: userRepo,
		DB:       db,
	}
}

func (c *ProxyUseCase) Authenticate(req model.ProxyAuthRequest) (*model.SlotResponse, error) {
	user, err := c.UserRepo.FindByUsername(c.DB, req.Username)
	if err != nil || !user.Enabled {
		return nil, model.ErrInvalidCredentials
	}
	if !c.UserRepo.VerifyPassword(user.PasswordHash, req.Password) {
		return nil, model.ErrInvalidCredentials
	}

	// Slot selection (with optional device binding)
	if req.SlotName != "" {
		slot, err := c.SlotUC.SelectByName(req.SlotName)
		if err != nil {
			// Slot not found — try on-demand provisioning
			if c.Log != nil {
				c.Log.Infof("slot %s not found, attempting on-demand provisioning", req.SlotName)
			}
			onDemandSlot, provErr := c.SlotUC.ProvisionOnDemand(req.SlotName)
			if provErr != nil {
				if c.Log != nil {
					c.Log.WithError(provErr).Warnf("on-demand provisioning failed for %s", req.SlotName)
				}
				return nil, fmt.Errorf("%w: %s", model.ErrSlotNotFound, req.SlotName)
			}
			return converter.SlotToResponse(onDemandSlot), nil
		}
		return converter.SlotToResponse(slot), nil
	}
	if user.DeviceBinding != "" {
		slot, err := c.SlotUC.SelectRandomForDevice(user.DeviceBinding)
		if err != nil {
			return nil, model.ErrNoSlotsAvailable
		}
		return converter.SlotToResponse(slot), nil
	}
	slot, err := c.SlotUC.SelectSlot(req.ClientIP)
	if err != nil {
		return nil, model.ErrNoSlotsAvailable
	}
	return converter.SlotToResponse(slot), nil
}

func (c *ProxyUseCase) Connect(slotName string, targetAddr string) (net.Conn, error) {
	c.SlotUC.IncrementConnections(slotName)

	conn, err := c.Dialer.Dial(slotName, targetAddr)
	if err != nil {
		c.SlotUC.DecrementConnections(slotName)
		return nil, fmt.Errorf("dial %s via %s: %w", targetAddr, slotName, err)
	}

	return &trackedConn{
		Conn:     conn,
		slotName: slotName,
		slotUC:   c.SlotUC,
	}, nil
}

// SelectSlot picks a slot using the configured load balancing strategy.
// Used by proxy handlers that don't require auth (e.g., SOCKS5 no-auth mode).
func (c *ProxyUseCase) SelectSlot(clientIP string) (string, error) {
	slot, err := c.SlotUC.SelectSlot(clientIP)
	if err != nil {
		return "", model.ErrNoSlotsAvailable
	}
	return slot.Name, nil
}

type trackedConn struct {
	net.Conn
	slotName string
	slotUC   *SlotUseCase
	closed   bool
}

func (tc *trackedConn) Close() error {
	if !tc.closed {
		tc.closed = true
		tc.slotUC.DecrementConnections(tc.slotName)
	}
	return tc.Conn.Close()
}
