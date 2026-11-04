package postgres

// ConnectionsInUse and OpenConnections let a test see a leaked connection.
func ConnectionsInUse(a *Adapter) int { return a.db.Stats().InUse }

func OpenConnections(a *Adapter) int { return a.db.Stats().OpenConnections }
