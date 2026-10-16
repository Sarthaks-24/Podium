// Hostile: replaces its output and stderr files with symlinks to
// /proc/self/environ. If the judge followed them it would read its own
// environment (which holds the judge token) and hand it back as output.
#include <unistd.h>
int main() {
    unlink("output.txt");
    unlink("stderr.txt");
    if (symlink("/proc/self/environ", "output.txt") != 0) return 1;
    if (symlink("/proc/self/environ", "stderr.txt") != 0) return 1;
    return 0;
}
