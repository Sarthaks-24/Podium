// Reads n then n integers, sorts them, prints the minimum, median, maximum.
import java.io.*;
import java.util.Arrays;

public class Main {
    public static void main(String[] args) throws IOException {
        StreamTokenizer in = new StreamTokenizer(new BufferedInputStream(System.in, 1 << 16));
        in.nextToken();
        int n = (int) in.nval;
        int[] a = new int[n];
        for (int i = 0; i < n; i++) {
            in.nextToken();
            a[i] = (int) in.nval;
        }
        Arrays.sort(a);
        System.out.println(a[0] + " " + a[n / 2] + " " + a[n - 1]);
    }
}
