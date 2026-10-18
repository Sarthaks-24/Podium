package config

// CheckPublicListen refuses, in production, a PODIUM_LISTEN that is not a
// loopback address. On the VM Caddy is the only thing that may reach the
// server: it terminates TLS, and the server trusts the client address in
// X-Forwarded-For only from a loopback peer. A server bound to a public
// address would serve plain http to the internet beside Caddy and let
// anyone set that header. Testing mode already requires loopback; dev mode
// may bind elsewhere for work on a private network.
//
// It is a separate check from LoadServer so it can be applied by
// podium-server's main; the error names the variable and the file like
// every other configuration error.
func CheckPublicListen(c Server) error {
	if c.Mode != ModeProduction || IsLoopback(c.Listen) {
		return nil
	}
	p := &Problems{Service: ServerService}
	p.add("PODIUM_LISTEN must be a loopback address such as 127.0.0.1:8080 in production: Caddy is the public listener and proxies to it")
	return p
}
