package community

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"time"
)

// The BBS list's online check: does anything answer at the address?
// A plain TCP connect, nothing sent. Addresses that resolve to a
// private, loopback or link-local network are never tried -- callers
// enter these, and the check mustn't become a way to probe the
// server's own network.

// CheckEvery is how often the whole list is checked.
var CheckEvery = time.Hour

// checkTimeout bounds one connect.
var checkTimeout = 8 * time.Second

// ErrPrivate: the address is on a private network.
var ErrPrivate = errors.New("community: a private address isn't checked")

// hostPort reads "host", "host:port", "telnet://host:port", "ssh://host"
// (default ports 23, 22 for ssh, 513 for rlogin).
func hostPort(address string) (string, string, error) {
	a := strings.TrimSpace(address)
	port := "23"
	if u, err := url.Parse(a); err == nil && u.Scheme != "" && u.Host != "" {
		switch strings.ToLower(u.Scheme) {
		case "ssh":
			port = "22"
		case "rlogin":
			port = "513"
		}
		a = u.Host
	}
	host, p, err := net.SplitHostPort(a)
	if err != nil {
		host, p = a, port
	}
	if n, err := strconv.Atoi(p); err != nil || n < 1 || n > 65535 {
		return "", "", fmt.Errorf("community: no port in %q", address)
	}
	if host == "" || strings.ContainsAny(host, " /") {
		return "", "", fmt.Errorf("community: no host in %q", address)
	}
	return host, p, nil
}

func private(ip net.IP) bool {
	return ip.IsLoopback() || ip.IsPrivate() || ip.IsLinkLocalUnicast() || ip.IsLinkLocalMulticast() ||
		ip.IsUnspecified() || ip.IsMulticast() ||
		// CGNAT
		(ip.To4() != nil && ip.To4()[0] == 100 && ip.To4()[1]&0xc0 == 64)
}

// Probe reports whether something answers at address.
func Probe(ctx context.Context, address string) (bool, error) {
	host, port, err := hostPort(address)
	if err != nil {
		return false, err
	}
	ctx, cancel := context.WithTimeout(ctx, checkTimeout)
	defer cancel()
	ips, err := net.DefaultResolver.LookupIP(ctx, "ip", host)
	if err != nil || len(ips) == 0 {
		return false, nil // no such name: not online
	}
	for _, ip := range ips {
		if private(ip) {
			return false, ErrPrivate
		}
	}
	var d net.Dialer
	conn, err := d.DialContext(ctx, "tcp", net.JoinHostPort(ips[0].String(), port))
	if err != nil {
		return false, nil
	}
	conn.Close()
	return true, nil
}

// RecordCheck stores a check's outcome.
func (s *Store) RecordCheck(id int64, online bool, at time.Time) error {
	if _, err := s.db.Exec(`UPDATE bbs_list SET checked_at = ?, online = ?,
			last_up_at = CASE WHEN ? THEN ? ELSE last_up_at END WHERE id = ?`,
		at.UnixMilli(), online, online, at.UnixMilli(), id); err != nil {
		return fmt.Errorf("community: %w", err)
	}
	return nil
}

// CheckBBSList checks the entries not checked within every (all with
// 0), four at a time.
func (s *Store) CheckBBSList(ctx context.Context, every time.Duration, probe func(context.Context, string) (bool, error)) error {
	list, err := s.BBSList()
	if err != nil {
		return err
	}
	sem := make(chan struct{}, 4)
	var wg sync.WaitGroup
	for _, b := range list {
		if every > 0 && !b.CheckedAt.IsZero() && time.Since(b.CheckedAt) < every {
			continue
		}
		wg.Add(1)
		sem <- struct{}{}
		go func(b BBS) {
			defer wg.Done()
			defer func() { <-sem }()
			up, err := probe(ctx, b.Address)
			if errors.Is(err, ErrPrivate) {
				up = false
			}
			s.RecordCheck(b.ID, up, time.Now())
		}(b)
	}
	wg.Wait()
	return nil
}

// RunChecks keeps checking: new or changed entries within a minute,
// the rest every CheckEvery.
func (s *Store) RunChecks(ctx context.Context) {
	t := time.NewTicker(time.Minute)
	defer t.Stop()
	for {
		s.CheckBBSList(ctx, CheckEvery, Probe)
		select {
		case <-ctx.Done():
			return
		case <-t.C:
		}
	}
}
