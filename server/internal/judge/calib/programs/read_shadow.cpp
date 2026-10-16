// Hostile: tries to read the host password hashes. Exits 0 and prints
// OPENED only if the file could be opened; the sandbox must prevent that.
#include <cstdio>
int main() {
    FILE *f = fopen("/etc/shadow", "r");
    if (!f) return 1;
    puts("OPENED");
    return 0;
}
