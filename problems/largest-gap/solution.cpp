#include <algorithm>
#include <cstdio>
#include <vector>

int main() {
    int n;
    scanf("%d", &n);
    std::vector<long long> v(n);
    for (auto &x : v) scanf("%lld", &x);
    std::sort(v.begin(), v.end());
    long long best = 0;
    for (int i = 1; i < n; i++) best = std::max(best, v[i] - v[i - 1]);
    printf("%lld\n", best);
}
