// Hostile: tries to fork forever. With --processes=1 every fork fails, so
// the loop spins until the time limit; the host must stay unaffected.
#include <unistd.h>
int main() {
    for (;;) fork();
}
