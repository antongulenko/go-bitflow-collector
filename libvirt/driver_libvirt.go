// +build !nolibvirt

package libvirt

import (
	"errors"
	"fmt"

	lib "github.com/libvirt/libvirt-go"
	log "github.com/sirupsen/logrus"
)

const (
	NO_FLAGS             = 0
	FETCH_DOMAINS_FLAGS  = lib.CONNECT_LIST_DOMAINS_ACTIVE | lib.CONNECT_LIST_DOMAINS_RUNNING
	MAX_NUM_MEMORY_STATS = 8
)

func NewDriver() Driver {
	return new(DriverImpl)
}

type DriverImpl struct {
	uri  string
	conn *lib.Connect
}

func (d *DriverImpl) Connect(uri string) error {
	d.uri = uri
	return nil
}

func (d *DriverImpl) ListDomains() ([]Domain, error) {
	conn, err := d.connection()
	if err != nil {
		return nil, err
	}
	virDomains, err := conn.ListAllDomains(FETCH_DOMAINS_FLAGS)
	if err != nil {
		return nil, err
	}
	domains := make([]Domain, len(virDomains))
	for i, domain := range virDomains {
		domains[i] = &DomainImpl{domain}
	}
	return domains, nil
}

func (d *DriverImpl) connection() (*lib.Connect, error) {
	conn := d.conn
	if conn != nil {
		if alive, err := conn.IsAlive(); err != nil || !alive {
			log.Warnln("Libvirt alive connection check failed:", err)
			if err := d.Close(); err != nil {
				return nil, err
			}
			conn = nil
		}
	}
	if conn == nil {
		if d.uri == "" {
			return nil, errors.New("Drier.Connect() has not yet been called.")
		}
		var err error
		conn, err = lib.NewConnect(d.uri)
		if err != nil {
			return nil, err
		}
		d.conn = conn
	}
	return conn, nil
}

func (d *DriverImpl) Close() (err error) {
	if d.conn != nil {
		_, err = d.conn.Close()
		d.conn = nil
	}
	return
}

type DomainImpl struct {
	domain lib.Domain
}

func (d *DomainImpl) GetName() (string, error) {
	return d.domain.GetName()
}

func (d *DomainImpl) CpuStats() (res VirDomainCpuStats, err error) {
	var statSlice []lib.DomainCPUStats
	statSlice, err = d.domain.GetCPUStats(-1, 1, NO_FLAGS)
	if err == nil && len(statSlice) != 1 {
		err = fmt.Errorf("Libvirt returned %v CPU stats instead of 1: %v", len(statSlice), statSlice)
	}
	if err == nil {
		stats := statSlice[0]
		res = VirDomainCpuStats{
			CpuTime:    stats.CpuTime,
			SystemTime: stats.SystemTime,
			UserTime:   stats.UserTime,
			VcpuTime:   stats.VcpuTime,
		}
	}
	return
}

func (d *DomainImpl) BlockStats(dev string) (res VirDomainBlockStats, err error) {
	var stats *lib.DomainBlockStats
	stats, err = d.domain.BlockStats(dev)
	if err == nil {
		res = VirDomainBlockStats{
			RdReq:   stats.RdReq,
			WrReq:   stats.WrReq,
			RdBytes: stats.RdBytes,
			WrBytes: stats.WrBytes,
		}
	}
	return
}

func (d *DomainImpl) BlockInfo(dev string) (res VirDomainBlockInfo, err error) {
	var stats *lib.DomainBlockInfo
	stats, err = d.domain.GetBlockInfo(dev, NO_FLAGS)
	if err == nil {
		res = VirDomainBlockInfo{
			Allocation: stats.Allocation,
			Capacity:   stats.Capacity,
			Physical:   stats.Physical,
		}
	}
	return
}

func (d *DomainImpl) MemoryStats() (res VirDomainMemoryStat, err error) {
	var stats []lib.DomainMemoryStat
	stats, err = d.domain.MemoryStats(MAX_NUM_MEMORY_STATS, NO_FLAGS)
	if err == nil {
		for _, stat := range stats {
			switch stat.Tag {
			case int32(lib.DOMAIN_MEMORY_STAT_UNUSED):
				res.Unused = stat.Val
			case int32(lib.DOMAIN_MEMORY_STAT_AVAILABLE):
				res.Available = stat.Val
			}
		}
	}
	return
}

func (d *DomainImpl) InterfaceStats(interfaceName string) (res VirDomainInterfaceStats, err error) {
	var stats *lib.DomainInterfaceStats
	stats, err = d.domain.InterfaceStats(interfaceName)
	if err == nil {
		res = VirDomainInterfaceStats{
			RxBytes:   stats.RxBytes,
			RxPackets: stats.RxPackets,
			RxErrs:    stats.RxErrs,
			RxDrop:    stats.RxDrop,
			TxBytes:   stats.TxBytes,
			TxPackets: stats.TxPackets,
			TxErrs:    stats.TxErrs,
			TxDrop:    stats.TxDrop,
		}
	}
	return
}

func (d *DomainImpl) GetXML() (string, error) {
	return d.domain.GetXMLDesc(NO_FLAGS)
}

func (d *DomainImpl) GetInfo() (res DomainInfo, err error) {
	var info *lib.DomainInfo
	info, err = d.domain.GetInfo()
	if err == nil {
		res.CpuTime = info.CpuTime
		res.MaxMem = info.MaxMem
		res.Mem = info.Memory
	}
	return
}
