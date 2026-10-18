package integrity

import (
	"strings"
	"testing"
)

// Calibration set (integrity plan section 6): six independent solutions
// to one problem (shortest path on a grid from S to E) in different
// styles, and three disguised copies. Copies must be found strong; no
// independent pair may be reported.
var independent = map[int64]Subject{
	1: {Language: "cpp17", Source: `#include <bits/stdc++.h>
using namespace std;
int main(){
    int n,m; cin>>n>>m;
    vector<string> g(n);
    for(auto&r:g) cin>>r;
    int sr=0,sc=0,er=0,ec=0;
    for(int i=0;i<n;i++) for(int j=0;j<m;j++){ if(g[i][j]=='S'){sr=i;sc=j;} if(g[i][j]=='E'){er=i;ec=j;} }
    vector<vector<int>> d(n, vector<int>(m,-1));
    queue<pair<int,int>> q; q.push({sr,sc}); d[sr][sc]=0;
    int dr[]={1,-1,0,0}, dc[]={0,0,1,-1};
    while(!q.empty()){
        auto [r,c]=q.front(); q.pop();
        for(int k=0;k<4;k++){
            int nr=r+dr[k], nc=c+dc[k];
            if(nr<0||nc<0||nr>=n||nc>=m) continue;
            if(g[nr][nc]=='#'||d[nr][nc]!=-1) continue;
            d[nr][nc]=d[r][c]+1; q.push({nr,nc});
        }
    }
    cout<<d[er][ec]<<"\n";
}
`},
	2: {Language: "cpp17", Source: `#include <cstdio>
#include <cstring>
const int MAXN = 1005;
char grid[MAXN][MAXN];
int dist[MAXN][MAXN];
int qx[MAXN*MAXN], qy[MAXN*MAXN];
int rows, cols;
bool inside(int x, int y) { return x >= 0 && y >= 0 && x < rows && y < cols; }
int bfs(int sx, int sy, int tx, int ty) {
    memset(dist, -1, sizeof dist);
    int head = 0, tail = 0;
    qx[tail] = sx; qy[tail] = sy; tail++;
    dist[sx][sy] = 0;
    const int mx[4] = {0, 1, 0, -1};
    const int my[4] = {1, 0, -1, 0};
    while (head < tail) {
        int x = qx[head], y = qy[head]; head++;
        if (x == tx && y == ty) return dist[x][y];
        for (int dir = 0; dir < 4; dir++) {
            int a = x + mx[dir], b = y + my[dir];
            if (inside(a, b) && grid[a][b] != '#' && dist[a][b] < 0) {
                dist[a][b] = dist[x][y] + 1;
                qx[tail] = a; qy[tail] = b; tail++;
            }
        }
    }
    return -1;
}
int main() {
    scanf("%d %d", &rows, &cols);
    int sx = 0, sy = 0, tx = 0, ty = 0;
    for (int i = 0; i < rows; i++) {
        scanf("%s", grid[i]);
        for (int j = 0; j < cols; j++) {
            if (grid[i][j] == 'S') { sx = i; sy = j; }
            else if (grid[i][j] == 'E') { tx = i; ty = j; }
        }
    }
    printf("%d\n", bfs(sx, sy, tx, ty));
    return 0;
}
`},
	3: {Language: "cpp17", Source: `#include <iostream>
#include <vector>
#include <deque>
#include <string>
using namespace std;
int main() {
    ios::sync_with_stdio(false);
    int h, w;
    cin >> h >> w;
    vector<string> a(h);
    int start = -1, goal = -1;
    for (int i = 0; i < h; ++i) {
        cin >> a[i];
        for (int j = 0; j < w; ++j) {
            if (a[i][j] == 'S') start = i * w + j;
            if (a[i][j] == 'E') goal = i * w + j;
        }
    }
    vector<int> best(h * w, 1 << 30);
    deque<int> dq;
    best[start] = 0;
    dq.push_back(start);
    auto relax = [&](int from, int to) {
        if (best[to] > best[from] + 1) {
            best[to] = best[from] + 1;
            dq.push_back(to);
        }
    };
    while (!dq.empty()) {
        int cur = dq.front();
        dq.pop_front();
        int i = cur / w, j = cur % w;
        if (i > 0 && a[i - 1][j] != '#') relax(cur, cur - w);
        if (i + 1 < h && a[i + 1][j] != '#') relax(cur, cur + w);
        if (j > 0 && a[i][j - 1] != '#') relax(cur, cur - 1);
        if (j + 1 < w && a[i][j + 1] != '#') relax(cur, cur + 1);
    }
    cout << (best[goal] == (1 << 30) ? -1 : best[goal]) << endl;
}
`},
	4: {Language: "cpp17", Source: `#include <bits/stdc++.h>
using namespace std;
typedef pair<int,int> P;
int main(){
  int R, C;
  if(!(cin >> R >> C)) return 0;
  vector<string> mp(R);
  P s, e;
  for(int r = 0; r < R; r++){
    cin >> mp[r];
    for(int c = 0; c < C; c++){
      if(mp[r][c] == 'S') s = P(r, c);
      if(mp[r][c] == 'E') e = P(r, c);
    }
  }
  set<P> seen; seen.insert(s);
  vector<P> frontier{s};
  int steps = 0;
  while(!frontier.empty()){
    vector<P> next;
    for(P p : frontier){
      if(p == e){ printf("%d\n", steps); return 0; }
      for(P d : {P(0,1), P(1,0), P(0,-1), P(-1,0)}){
        P q(p.first + d.first, p.second + d.second);
        if(q.first < 0 || q.first >= R || q.second < 0 || q.second >= C) continue;
        if(mp[q.first][q.second] == '#' || seen.count(q)) continue;
        seen.insert(q);
        next.push_back(q);
      }
    }
    frontier.swap(next);
    steps++;
  }
  puts("-1");
}
`},
	5: {Language: "python3", Source: `import sys
from collections import deque

def main():
    data = sys.stdin.read().split()
    n, m = int(data[0]), int(data[1])
    grid = data[2:2 + n]
    for i, row in enumerate(grid):
        if 'S' in row:
            start = (i, row.index('S'))
        if 'E' in row:
            end = (i, row.index('E'))
    dist = {start: 0}
    queue = deque([start])
    while queue:
        r, c = queue.popleft()
        if (r, c) == end:
            print(dist[(r, c)])
            return
        for dr, dc in ((1, 0), (-1, 0), (0, 1), (0, -1)):
            nr, nc = r + dr, c + dc
            if 0 <= nr < n and 0 <= nc < m and grid[nr][nc] != '#' and (nr, nc) not in dist:
                dist[(nr, nc)] = dist[(r, c)] + 1
                queue.append((nr, nc))
    print(-1)

main()
`},
	6: {Language: "python3", Source: `n, m = map(int, input().split())
rows = [input().strip() for _ in range(n)]
INF = float('inf')
cost = [[INF] * m for _ in range(n)]
todo = []
for y in range(n):
    for x in range(m):
        if rows[y][x] == 'S':
            cost[y][x] = 0
            todo.append((y, x))
        elif rows[y][x] == 'E':
            target = (y, x)
k = 0
while k < len(todo):
    y, x = todo[k]
    k += 1
    for ny, nx in [(y + 1, x), (y - 1, x), (y, x + 1), (y, x - 1)]:
        if 0 <= ny < n and 0 <= nx < m and rows[ny][nx] != '#' and cost[ny][nx] == INF:
            cost[ny][nx] = cost[y][x] + 1
            todo.append((ny, nx))
ty, tx = target
print(cost[ty][tx] if cost[ty][tx] != INF else -1)
`},
}

// copies maps a copier to the student copied and the disguised program.
var copies = map[int64]struct {
	of  int64
	sub Subject
}{
	// Renamed everything, new comments and layout, globals reordered.
	7: {2, Subject{Language: "cpp17", Source: `#include <cstdio>
#include <cstring>
// my own bfs solution
const int LIM = 1005;
int R, C;
int step[LIM][LIM];
char board[LIM][LIM];
int fx[LIM*LIM], fy[LIM*LIM];
bool ok(int r, int c) { return r >= 0 && c >= 0 && r < R && c < C; }
int solve(int ar, int ac, int br, int bc) {
    memset(step, -1, sizeof step);
    int lo = 0, hi = 0;
    fx[hi] = ar; fy[hi] = ac; hi++;
    step[ar][ac] = 0;
    const int vr[4] = {0, 1, 0, -1};   // right, down, left, up
    const int vc[4] = {1, 0, -1, 0};
    while (lo < hi) {
        int r = fx[lo], c = fy[lo]; lo++;
        if (r == br && c == bc) return step[r][c];
        for (int t = 0; t < 4; t++) {
            int u = r + vr[t], v = c + vc[t];
            if (ok(u, v) && board[u][v] != '#' && step[u][v] < 0) {
                step[u][v] = step[r][c] + 1;
                fx[hi] = u; fy[hi] = v; hi++;
            }
        }
    }
    return -1;
}
int main() {
    scanf("%d %d", &R, &C);
    int ar = 0, ac = 0, br = 0, bc = 0;
    for (int i = 0; i < R; i++) {
        scanf("%s", board[i]);
        for (int j = 0; j < C; j++) {
            if (board[i][j] == 'S') { ar = i; ac = j; }
            else if (board[i][j] == 'E') { br = i; bc = j; }
        }
    }
    printf("%d\n", solve(ar, ac, br, bc));
    return 0;
}
`}},
	// Renamed, dead code inserted, one loop rewritten.
	8: {1, Subject{Language: "cpp17", Source: `#include <bits/stdc++.h>
using namespace std;
int unused_helper(int v){ return v * 2; }
int main(){
    int rows,cols; cin>>rows>>cols;
    vector<string> grid(rows);
    for(int i=0;i<rows;i++) cin>>grid[i];
    int a=0,b=0,x=0,y=0;
    long long checksum = 0;
    for(int i=0;i<rows;i++) for(int j=0;j<cols;j++){ if(grid[i][j]=='S'){a=i;b=j;} if(grid[i][j]=='E'){x=i;y=j;} }
    vector<vector<int>> dd(rows, vector<int>(cols,-1));
    queue<pair<int,int>> bfs; bfs.push({a,b}); dd[a][b]=0;
    int di[]={1,-1,0,0}, dj[]={0,0,1,-1};
    while(!bfs.empty()){
        auto [u,v]=bfs.front(); bfs.pop();
        checksum += u;
        for(int t=0;t<4;t++){
            int nu=u+di[t], nv=v+dj[t];
            if(nu<0||nv<0||nu>=rows||nv>=cols) continue;
            if(grid[nu][nv]=='#'||dd[nu][nv]!=-1) continue;
            dd[nu][nv]=dd[u][v]+1; bfs.push({nu,nv});
        }
    }
    cout<<dd[x][y]<<"\n";
}
`}},
	// Renamed with comments, direction order swapped.
	9: {5, Subject{Language: "python3", Source: `import sys
from collections import deque

# read everything at once
def main():
    tokens = sys.stdin.read().split()
    h, w = int(tokens[0]), int(tokens[1])
    board = tokens[2:2 + h]
    for y, line in enumerate(board):
        if 'S' in line:
            src = (y, line.index('S'))
        if 'E' in line:
            dst = (y, line.index('E'))
    seen = {src: 0}
    q = deque([src])
    while q:
        a, b = q.popleft()
        if (a, b) == dst:
            print(seen[(a, b)])
            return
        for da, db in ((0, 1), (0, -1), (1, 0), (-1, 0)):
            na, nb = a + da, b + db
            if 0 <= na < h and 0 <= nb < w and board[na][nb] != '#' and (na, nb) not in seen:
                seen[(na, nb)] = seen[(a, b)] + 1
                q.append((na, nb))
    print(-1)

main()
`}},
}

func calibrationSubjects() []Subject {
	var subs []Subject
	for id, s := range independent {
		s.UserID, s.SubmissionID = id, id*100
		subs = append(subs, s)
	}
	for id, c := range copies {
		s := c.sub
		s.UserID, s.SubmissionID = id, id*100
		subs = append(subs, s)
	}
	return subs
}

func TestCalibrationFindsCopiesOnly(t *testing.T) {
	pairs := Compare(calibrationSubjects(), nil, Defaults)
	found := map[[2]int64]Pair{}
	for _, p := range pairs {
		found[[2]int64{p.A.UserID, p.B.UserID}] = p
		t.Logf("%d and %d: %.2f %s (%d shared)", p.A.UserID, p.B.UserID, p.Score, p.Strength, p.Shared)
	}
	for copier, c := range copies {
		p, ok := found[[2]int64{c.of, copier}]
		if !ok {
			t.Errorf("copy by %d of %d not found", copier, c.of)
			continue
		}
		if p.Strength != "strong" {
			t.Errorf("copy by %d of %d only %s: %.2f with %d shared", copier, c.of, p.Strength, p.Score, p.Shared)
		}
		if len(p.SpansA) == 0 || len(p.SpansB) == 0 {
			t.Errorf("copy by %d of %d has no matched spans", copier, c.of)
		}
		delete(found, [2]int64{c.of, copier})
	}
	for k, p := range found {
		t.Errorf("independent pair %v reported at %.2f", k, p.Score)
	}
	clusters := Clusters(pairs)
	if len(clusters) != 3 {
		t.Errorf("clusters %v, want 3 pairs", clusters)
	}

	// The margin: with no threshold, independent programs stay well
	// below the reporting line.
	all := Defaults
	all.Threshold = 0
	worst := 0.0
	for _, p := range Compare(calibrationSubjects(), nil, all) {
		_, a := independent[p.A.UserID]
		_, b := independent[p.B.UserID]
		if a && b && p.Score > worst {
			worst = p.Score
		}
	}
	t.Logf("most similar independent pair: %.2f", worst)
	if worst >= Defaults.Threshold-0.2 {
		t.Errorf("independent programs reach %.2f, too close to the %.2f threshold", worst, Defaults.Threshold)
	}
}

// Code everyone may share (the reference, starter code) never counts: a
// student whose program is the reference with new names is not linked to
// another who did the same.
func TestBaseProgramsDoNotCount(t *testing.T) {
	ref := independent[1]
	pairs := Compare(calibrationSubjects(), []Subject{ref}, Defaults)
	for _, p := range pairs {
		if p.A.UserID == 1 && p.B.UserID == 8 {
			t.Errorf("pair built from the reference reported at %.2f", p.Score)
		}
	}
}

// Short programs, such as A plus B, look alike whoever writes them.
func TestShortProgramsAreSkipped(t *testing.T) {
	src := "#include <cstdio>\nint main(){long long a,b;scanf(\"%lld %lld\",&a,&b);printf(\"%lld\\n\",a+b);}\n"
	var subs []Subject
	for u := int64(1); u <= 4; u++ {
		subs = append(subs, Subject{SubmissionID: u, UserID: u, Language: "cpp17", Source: src})
	}
	if pairs := Compare(subs, nil, Defaults); len(pairs) != 0 {
		t.Errorf("short identical programs reported: %d pairs", len(pairs))
	}
}

// A fingerprint most students share is an idiom, not evidence.
func TestCommonFingerprintsAreDropped(t *testing.T) {
	prog := independent[2].Source
	var subs []Subject
	for u := int64(1); u <= 12; u++ {
		// Every student submits the same program with a different name
		// for one variable: all fingerprints are common to everyone.
		subs = append(subs, Subject{SubmissionID: u, UserID: u, Language: "cpp17",
			Source: strings.ReplaceAll(prog, "tail", "t"+strings.Repeat("x", int(u)))})
	}
	if pairs := Compare(subs, nil, Defaults); len(pairs) != 0 {
		t.Errorf("a program everyone shares produced %d pairs", len(pairs))
	}
}
