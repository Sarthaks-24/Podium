// Reads n then n integers, sorts them, prints the minimum, median, maximum.
#include <algorithm>
#include <cstdio>
#include <vector>
int main() {
    int n;
    if (scanf("%d", &n) != 1) return 1;
    std::vector<int> a(n);
    for (int &x : a) scanf("%d", &x);
    std::sort(a.begin(), a.end());
    printf("%d %d %d\n", a[0], a[n / 2], a[n - 1]);
}
