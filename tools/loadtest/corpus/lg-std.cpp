#include <bits/stdc++.h>
using namespace std;

int main() {
    int n;
    scanf("%d", &n);
    vector<long long> v(n);
    for (auto &x : v) scanf("%lld", &x);
    sort(v.begin(), v.end());
    long long best = 0;
    for (int i = 1; i < n; i++) best = max(best, v[i] - v[i - 1]);
    printf("%lld\n", best);
}
