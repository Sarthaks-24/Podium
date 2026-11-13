#include <cstdio>
#include <vector>

// For each value, scans every other value for the next larger one: right on
// small inputs, far too slow for n = 200000.
int main() {
    int n;
    scanf("%d", &n);
    std::vector<long long> v(n);
    for (auto &x : v) scanf("%lld", &x);
    long long best = 0;
    for (int i = 0; i < n; i++) {
        long long next = -1;
        for (int j = 0; j < n; j++)
            if (v[j] > v[i] && (next < 0 || v[j] < next)) next = v[j];
        if (next >= 0 && next - v[i] > best) best = next - v[i];
    }
    printf("%lld\n", best);
}
