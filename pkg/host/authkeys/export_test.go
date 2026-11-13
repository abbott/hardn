package authkeys

// ReadGuarded is readGuarded, for the test that reads a FIFO on this machine:
// ReadAccounts reads /etc/passwd, which a test cannot stand in for there.
var ReadGuarded = readGuarded
