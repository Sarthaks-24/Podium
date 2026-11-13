// A correct program that uses about 1.2 GB on a problem with the largest
// memory limit (1,024 MB): a heap that is touched through, and threads
// whose stacks grow. The box allows 1,280 MB for it (the limit plus the
// JVM's 256 MB), so it must not be killed.
public class Main {
    static long depth(int n) {
        return n == 0 ? 1 : depth(n - 1) + 1;
    }

    public static void main(String[] args) throws Exception {
        byte[][] live = new byte[80][];
        for (int i = 0; i < live.length; i++) {
            live[i] = new byte[8 << 20];
            java.util.Arrays.fill(live[i], (byte) i);
        }
        long sum = 0;
        for (int i = 0; i < 200; i++) {
            byte[] garbage = new byte[4 << 20];
            java.util.Arrays.fill(garbage, (byte) 1);
            sum += garbage[i];
        }
        long[] deep = new long[THREADS];
        Thread[] threads = new Thread[THREADS];
        for (int t = 0; t < THREADS; t++) {
            final int id = t;
            threads[t] = new Thread(null, () -> deep[id] = depth(DEPTH), "deep" + t, 64L << 20);
            threads[t].start();
        }
        for (Thread t : threads) t.join();
        for (long d : deep) sum += d;
        System.out.println(sum + live[79][0]);
    }

    static final int THREADS = 6;
    static final int DEPTH = 600000;
}
