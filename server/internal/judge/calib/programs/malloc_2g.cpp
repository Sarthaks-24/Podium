// Hostile: allocates 2 GB and touches every page. The volatile pointer and
// the printed sum stop -O2 from removing the allocation.
#include <cstdio>
#include <cstdlib>
int main() {
    const size_t n = 2ULL << 30;
    volatile char *p = static_cast<volatile char *>(malloc(n));
    if (!p) return 3;
    unsigned long sum = 0;
    for (size_t i = 0; i < n; i += 4096) p[i] = static_cast<char>(i >> 12);
    for (size_t i = 0; i < n; i += 4096) sum += p[i];
    printf("%lu\n", sum);
}
