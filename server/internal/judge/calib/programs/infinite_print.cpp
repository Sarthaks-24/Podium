// Hostile: prints forever, to hit the 16 MB output cap.
#include <cstdio>
int main() {
    for (;;) fputs("podium podium podium podium podium podium podium podium\n", stdout);
}
