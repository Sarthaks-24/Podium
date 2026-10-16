// Hostile: tries to reach the internet. Exits 0 and prints CONNECTED only if
// a TCP connection to 1.1.1.1:53 succeeds; the sandbox must prevent that.
#include <arpa/inet.h>
#include <cstdio>
#include <sys/socket.h>
#include <unistd.h>
int main() {
    int s = socket(AF_INET, SOCK_STREAM, 0);
    if (s < 0) return 1;
    sockaddr_in a{};
    a.sin_family = AF_INET;
    a.sin_port = htons(53);
    inet_pton(AF_INET, "1.1.1.1", &a.sin_addr);
    if (connect(s, reinterpret_cast<sockaddr *>(&a), sizeof a) != 0) return 1;
    puts("CONNECTED");
    close(s);
    return 0;
}
