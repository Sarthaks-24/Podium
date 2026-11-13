#include <cstdio>
#include <ctime>

// Burns 800 ms of CPU time before answering: 80 percent of the 1 s limit,
// whatever the machine. clock() counts the process's CPU time, so a loaded
// machine does not change it.
int main() {
    clock_t end = clock() + (clock_t)(0.80 * CLOCKS_PER_SEC);
    while (clock() < end) {}
    int t;
    scanf("%d", &t);
    while (t--) {
        long long a, b;
        scanf("%lld %lld", &a, &b);
        printf("%lld\n", a + b);
    }
}
