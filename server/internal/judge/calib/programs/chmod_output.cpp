// Hostile: writes some output, then makes its output and stderr files
// unreadable. The judge must treat them as empty, not fail the job: a
// program choosing when the judge breaks would leak a bit of the hidden
// input in exam mode.
#include <cstdio>
#include <sys/stat.h>
#include <unistd.h>
int main() {
    std::printf("partial\n");
    std::fflush(stdout);
    fchmod(1, 0);
    fchmod(2, 0);
    chmod("output.txt", 0);
    chmod("stderr.txt", 0);
    return 0;
}
