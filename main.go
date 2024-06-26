package main

import (
	"encoding/json"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/emersion/go-imap/v2"
	"github.com/emersion/go-imap/v2/imapclient"
)

type Account struct {
	Imap     string
	Username string
	Password string
	Include  []string
	Exclude  []string
}

type Config struct {
	Dir      string
	Debug    bool
	Accounts []*Account
}

func main() {
	config, err := load()
	if err != nil {
		slog.Error("load config error.", "err", err)
		return
	}
	for _, account := range config.Accounts {
		slog.Info("archive start.", "username", account.Username)
		err := archive(account, config.Dir, config.Debug)
		if err != nil {
			slog.Error("archive error.", "err", err, "username", account.Username)
			return
		}
		slog.Info("archive success.", "username", account.Username)
	}
}

func load() (*Config, error) {
	b, err := os.ReadFile("config.json")
	if err != nil {
		return nil, err
	}
	config := &Config{}
	err = json.Unmarshal(b, config)
	if err != nil {
		return nil, err
	}
	return config, nil
}

func archive(account *Account, dir string, debug bool) error {
	path := filepath.Join(dir, account.Username)
	err := os.MkdirAll(path, 0755)
	if err != nil {
		if !os.IsExist(err) {
			return fmt.Errorf("mkdir: %w", err)
		}
	}
	var option *imapclient.Options
	if debug {
		option = &imapclient.Options{DebugWriter: os.Stdout}
	}
	client, err := imapclient.DialTLS(account.Imap, option)
	if err != nil {
		return fmt.Errorf("dial: %w", err)
	}
	defer client.Close()
	slog.Info("dial tls success.")

	client.WaitGreeting()
	slog.Info("wait greeting success.")

	err = client.Login(account.Username, account.Password).Wait()
	if err != nil {
		return fmt.Errorf("login: %w", err)
	}
	slog.Info("login success.", "username", account.Username)

	if client.Caps().Has(imap.CapID) {
		if err := client.Id("name", "mail-archiver", "version", "1.0.0").Wait(); err != nil {
			return fmt.Errorf("id: %w", err)
		}
		slog.Info("id success.")
	}

	lds, err := client.List("", "*", nil).Collect()
	if err != nil {
		return fmt.Errorf("list: %w", err)
	}
	for _, ld := range lds {
		if skip(account, ld.Mailbox) {
			slog.Info("skip.", "username", account.Username, "mailbox", ld.Mailbox)
			continue
		}

		sd, err := client.Select(ld.Mailbox, nil).Wait()
		if err != nil {
			slog.Error("select error.", "username", account.Username, "mailbox", ld.Mailbox, "err", err)
			return err
		}
		slog.Info("select success.", "username", account.Username, "mailbox", ld.Mailbox, "messages", sd.NumMessages)

		mailboxpath := filepath.Join(path, ld.Mailbox)
		err = os.MkdirAll(mailboxpath, 0755)
		if err != nil {
			if !os.IsExist(err) {
				return fmt.Errorf("mkdir: %w", err)
			}
		}

		ignoreuids := make(map[imap.UID]bool)
		entries, err := os.ReadDir(mailboxpath)
		if err != nil {
			return err
		}
		for _, entry := range entries {
			if entry.IsDir() {
				continue
			}
			if filepath.Ext(entry.Name()) != ".eml" {
				continue
			}
			uidstr := strings.TrimSuffix(entry.Name(), ".eml")
			num, err := strconv.ParseUint(uidstr, 10, 32)
			if err == nil && num > 0 {
				ignoreuids[imap.UID(num)] = true
				slog.Info("ignore uid.", "username", account.Username, "mailbox", ld.Mailbox, "uid", num)
			}
		}

		shd, err := client.UIDSearch(&imap.SearchCriteria{}, &imap.SearchOptions{}).Wait()
		if err != nil {
			slog.Error("uid search error.", "username", account.Username, "mailbox", ld.Mailbox, "err", err)
			return err
		}
		var uids []imap.UID
		for _, uid := range shd.AllUIDs() {
			if !ignoreuids[uid] {
				uids = append(uids, uid)
			}
		}

		for _, uid := range uids {
			var seq imap.UIDSet
			seq.AddNum(uid)
			opt := &imap.FetchOptions{
				Envelope:    false,
				RFC822Size:  true,
				UID:         true,
				BodySection: []*imap.FetchItemBodySection{{Peek: true}},
			}
			msgs, err := client.Fetch(seq, opt).Collect()
			if err != nil {
				slog.Error("fetch error.", "username", account.Username, "mailbox", ld.Mailbox, "seq", seq, "err", err)
				return err
			}

			for _, msg := range msgs {
				if uid != msg.UID {
					slog.Error("invalid uid.", "username", account.Username, "mailbox", ld.Mailbox, "uid", uid, "msg.uid", msg.UID)
					continue
				}
				if len(msg.BodySection) != 1 {
					slog.Error("body section error.", "username", account.Username, "mailbox", ld.Mailbox, "uid", uid, "bodysections", len(msg.BodySection))
					return fmt.Errorf("body section error")
				}
				var body []byte
				for _, v := range msg.BodySection {
					body = v
				}
				slog.Info("fetch success.", "username", account.Username, "mailbox", ld.Mailbox, "uid", uid, "rfc822size", msg.RFC822Size, "bodysize", len(body))

				name := filepath.Join(mailboxpath, fmt.Sprintf("%d.eml", uid))
				err = os.WriteFile(name, body, 0644)
				if err != nil {
					slog.Error("write file error.", "username", account.Username, "mailbox", ld.Mailbox, "uid", uid, "rfc822size", msg.RFC822Size, "bodysize", len(body), "err", err)
					return err
				}
				slog.Info("write file success.", "username", account.Username, "mailbox", ld.Mailbox, "uid", uid, "rfc822size", msg.RFC822Size, "bodysize", len(body))
			}
		}
	}
	err = client.Logout().Wait()
	if err != nil {
		return fmt.Errorf("logout: %w", err)
	}
	return nil
}

func match(s []string, e string) bool {
	for _, v := range s {
		if v == e {
			return true
		}
	}
	return false
}

func skip(account *Account, mailbox string) bool {
	if match(account.Exclude, mailbox) {
		return true
	}
	if len(account.Include) > 0 && !match(account.Include, mailbox) {
		return true
	}
	return false
}
