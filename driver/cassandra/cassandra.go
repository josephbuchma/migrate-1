// Package cassandra implements the Driver interface.
package cassandra

import (
	"fmt"
	"net/url"
	"strings"
	"time"

	"github.com/gocql/gocql"
	"github.com/mattes/migrate/driver"
	"github.com/mattes/migrate/file"
	"github.com/mattes/migrate/migrate/direction"
)

type Driver struct {
	session *gocql.Session
}

const (
	tableName = "schema_migrations"
)

// Cassandra Driver URL format:
// cassandra://host:port/keyspace
//
// Example:
// cassandra://localhost/SpaceOfKeys
func (driver *Driver) Initialize(rawurl string) error {
	u, err := url.Parse(rawurl)

	cluster := gocql.NewCluster(u.Host)
	cluster.Keyspace = u.Path[1:len(u.Path)]
	cluster.Consistency = gocql.All
	cluster.Timeout = 1 * time.Minute

	// Check if url user struct is null
	if u.User != nil {
		password, passwordSet := u.User.Password()

		if passwordSet == false {
			return fmt.Errorf("Missing password. Please provide password.")
		}

		cluster.Authenticator = gocql.PasswordAuthenticator{
			Username: u.User.Username(),
			Password: password,
		}

	}

	driver.session, err = cluster.CreateSession()

	if err != nil {
		return err
	}

	if err := driver.ensureVersionTableExists(); err != nil {
		return err
	}
	return nil
}

func (driver *Driver) Close() error {
	driver.session.Close()
	return nil
}

func (driver *Driver) ensureVersionTableExists() error {
	err := driver.session.Query("CREATE TABLE IF NOT EXISTS " + tableName + " (version bigint primary key);").Exec()
	return err
}

func (driver *Driver) FilenameExtension() string {
	return "cql"
}

func (driver *Driver) Migrate(f file.File, pipe chan interface{}) {
	var err error
	defer func() {
		if err != nil {
			// Invert version direction if we couldn't apply the changes for some reason.
			if errRollback := driver.session.Query("DELETE FROM "+tableName+" WHERE version = ?", f.Version).Exec(); errRollback != nil {
				pipe <- errRollback
			}
			pipe <- err
		}
		close(pipe)
	}()

	pipe <- f

	if err = f.ReadContent(); err != nil {
		return
	}

	if f.Direction == direction.Up {
		if err := driver.session.Query("INSERT INTO "+tableName+" (version) VALUES (?)", f.Version).Exec(); err != nil {
			pipe <- err
			return
		}
	} else if f.Direction == direction.Down {
		if err := driver.session.Query("DELETE FROM "+tableName+" WHERE version = ?", f.Version).Exec(); err != nil {
			pipe <- err
			return
		}
	}

	for _, query := range strings.Split(string(f.Content), ";") {
		query = strings.TrimSpace(query)
		if len(query) == 0 {
			continue
		}

		if err = driver.session.Query(query).Exec(); err != nil {
			return
		}
	}
}

func (driver *Driver) Version() (uint64, error) {
	var version int64
	err := driver.session.Query("SELECT version FROM " + tableName).Scan(&version)
	return uint64(version), err
}

func (driver *Driver) Versions() ([]uint64, error) {
	versions := []uint64{}
	iter := driver.session.Query("SELECT version FROM " + tableName).Iter()
	var version int64
	for iter.Scan(&version) {
		versions = append(versions, uint64(version))
	}
	err := iter.Close()
	return versions, err
}

func init() {
	driver.RegisterDriver("cassandra", &Driver{})
}
