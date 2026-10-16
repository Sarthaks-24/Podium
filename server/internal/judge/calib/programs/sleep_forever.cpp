// Hostile: uses no CPU but never exits; only the wall-clock limit stops it.
#include <unistd.h>
int main() {
    for (;;) sleep(100);
}
