package repository

import (
	"database/sql"
	"fmt"
	"time"

	"github.com/sirupsen/logrus"

	"github.com/riakgu/moxy/internal/entity"
)

type DeviceRepository struct {
	Log *logrus.Logger
}

func NewDeviceRepository(log *logrus.Logger) *DeviceRepository {
	return &DeviceRepository{Log: log}
}

func (r *DeviceRepository) Create(db *sql.DB, device *entity.Device) error {
	now := time.Now().UnixMilli()
	device.CreatedAt = now
	device.UpdatedAt = now
	_, err := db.Exec(
		`INSERT INTO devices (id, serial, alias, carrier, interface, nameserver, nat64_prefix, status, max_slots, created_at, updated_at)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		device.ID, device.Serial, device.Alias, device.Carrier, device.Interface,
		device.Nameserver, device.NAT64Prefix, device.Status, device.MaxSlots,
		device.CreatedAt, device.UpdatedAt,
	)
	return err
}

func (r *DeviceRepository) FindAll(db *sql.DB) ([]*entity.Device, error) {
	rows, err := db.Query("SELECT id, serial, alias, carrier, interface, nameserver, nat64_prefix, status, max_slots, created_at, updated_at FROM devices ORDER BY created_at")
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var devices []*entity.Device
	for rows.Next() {
		d := &entity.Device{}
		if err := rows.Scan(&d.ID, &d.Serial, &d.Alias, &d.Carrier, &d.Interface,
			&d.Nameserver, &d.NAT64Prefix, &d.Status, &d.MaxSlots,
			&d.CreatedAt, &d.UpdatedAt); err != nil {
			return nil, err
		}
		devices = append(devices, d)
	}
	return devices, rows.Err()
}

func (r *DeviceRepository) FindByID(db *sql.DB, id string) (*entity.Device, error) {
	d := &entity.Device{}
	err := db.QueryRow(
		"SELECT id, serial, alias, carrier, interface, nameserver, nat64_prefix, status, max_slots, created_at, updated_at FROM devices WHERE id = ?", id,
	).Scan(&d.ID, &d.Serial, &d.Alias, &d.Carrier, &d.Interface,
		&d.Nameserver, &d.NAT64Prefix, &d.Status, &d.MaxSlots,
		&d.CreatedAt, &d.UpdatedAt)
	if err == sql.ErrNoRows {
		return nil, fmt.Errorf("device not found: %s", id)
	}
	return d, err
}

func (r *DeviceRepository) FindBySerial(db *sql.DB, serial string) (*entity.Device, error) {
	d := &entity.Device{}
	err := db.QueryRow(
		"SELECT id, serial, alias, carrier, interface, nameserver, nat64_prefix, status, max_slots, created_at, updated_at FROM devices WHERE serial = ?", serial,
	).Scan(&d.ID, &d.Serial, &d.Alias, &d.Carrier, &d.Interface,
		&d.Nameserver, &d.NAT64Prefix, &d.Status, &d.MaxSlots,
		&d.CreatedAt, &d.UpdatedAt)
	if err == sql.ErrNoRows {
		return nil, fmt.Errorf("device not found by serial: %s", serial)
	}
	return d, err
}

func (r *DeviceRepository) Update(db *sql.DB, device *entity.Device) error {
	device.UpdatedAt = time.Now().UnixMilli()
	_, err := db.Exec(
		`UPDATE devices SET serial=?, alias=?, carrier=?, interface=?, nameserver=?, nat64_prefix=?, status=?, max_slots=?, updated_at=? WHERE id=?`,
		device.Serial, device.Alias, device.Carrier, device.Interface,
		device.Nameserver, device.NAT64Prefix, device.Status, device.MaxSlots,
		device.UpdatedAt, device.ID,
	)
	return err
}

func (r *DeviceRepository) Delete(db *sql.DB, id string) error {
	_, err := db.Exec("DELETE FROM devices WHERE id = ?", id)
	return err
}
