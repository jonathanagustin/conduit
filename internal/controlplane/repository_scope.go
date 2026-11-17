package controlplane

// InTransaction reports whether this handle is a transaction view.
// Scope is local to a repository handle, never part of its persisted state.
// Applying a committed snapshot must not turn the owning handle into a
// transaction view. Nested transactions keep their enclosing view local.
func (repository *MemoryRepository) InTransaction() bool   { return repository.transactionLocal }
func (repository *FileRepository) InTransaction() bool     { return repository.transactionLocal }
func (repository *PostgresRepository) InTransaction() bool { return false }
func (view *authorizationAuditView) InTransaction() bool   { return true }
