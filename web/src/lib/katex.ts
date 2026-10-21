/**
 * KaTeX with its stylesheet, imported by lib/markdown.ts only when a
 * rendered text holds maths (F8.2), so a contest description or an
 * announcement without a formula never downloads either.
 */
import katex from 'katex';
import 'katex/dist/katex.min.css';

export default katex;
