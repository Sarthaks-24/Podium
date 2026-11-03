// Hostile: nests 1,500 directories inside its box (audit A16). isolate
// 2.2.1 walks a box with one open directory per level, so under the judge
// unit's soft limit of 1,024 open files its walk after the run fails and
// the box cannot be cleaned or initialised again until it is reset.
#include <sys/stat.h>
#include <unistd.h>
int main() {
    for (int i = 0; i < 1500; i++) {
        if (mkdir("a", 0777) != 0 || chdir("a") != 0) return 1;
    }
    return 0;
}
