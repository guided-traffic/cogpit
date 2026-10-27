package testkit

// RefusedAddr is a loopback address that refuses every connection at once: nothing listens on
// port 1, and it lies below every system's range of ports handed out for port 0 (Linux
// 32768–60999, macOS and Windows 49152–65535), so no listener of any test process on the computer
// gets it. A port freed by closing a listener is no such address: the next listener on port 0, in
// this test process or another package's, may get it, and one that accepts and stays silent turns
// the refusal into a timeout. A port bound without listening is none either: macOS drops the
// connection attempt instead of refusing it.
const RefusedAddr = "127.0.0.1:1"
