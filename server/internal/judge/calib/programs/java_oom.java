// Hostile: allocates 2 GB on a heap capped by -Xmx.
public class Main {
    public static void main(String[] args) {
        long[] a = new long[1 << 28];
        a[a.length - 1] = 1;
        System.out.println(a[a.length - 1]);
    }
}
