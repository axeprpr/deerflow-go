package harnessruntime

import "database/sql"

type sqlitePlaneRunSnapshotStore struct {
	*SQLiteRunSnapshotStore
	closer *sqliteStateCloser
}

func (s *sqlitePlaneRunSnapshotStore) Close() error {
	if s == nil || s.closer == nil {
		return nil
	}
	return s.closer.Close()
}

type sqlitePlaneRunEventStore struct {
	*SQLiteRunEventStore
	closer *sqliteStateCloser
}

func (s *sqlitePlaneRunEventStore) Close() error {
	if s == nil || s.closer == nil {
		return nil
	}
	return s.closer.Close()
}

type sqlitePlaneThreadStateStore struct {
	*SQLiteThreadStateStore
	closer *sqliteStateCloser
}

func (s *sqlitePlaneThreadStateStore) Close() error {
	if s == nil || s.closer == nil {
		return nil
	}
	return s.closer.Close()
}

func newSQLiteRuntimeStatePlane(path string) (RuntimeStatePlane, error) {
	db, err := openSQLiteStateDB(path, sqliteRuntimeStateSchemaSQL)
	if err != nil {
		return RuntimeStatePlane{}, err
	}
	return newSQLiteRuntimeStatePlaneWithDB(db), nil
}

func newSQLiteRuntimeStatePlaneWithDB(db *sql.DB) RuntimeStatePlane {
	closer := newSQLiteStateCloser(db)
	return RuntimeStatePlane{
		Snapshots: &sqlitePlaneRunSnapshotStore{
			SQLiteRunSnapshotStore: newSQLiteRunSnapshotStoreWithDB(db),
			closer:                 closer,
		},
		Events: &sqlitePlaneRunEventStore{
			SQLiteRunEventStore: newSQLiteRunEventStoreWithDB(db),
			closer:              closer,
		},
		Threads: &sqlitePlaneThreadStateStore{
			SQLiteThreadStateStore: newSQLiteThreadStateStoreWithDB(db),
			closer:                 closer,
		},
	}
}
