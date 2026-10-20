import { mount } from 'svelte';
import './styles/tokens.css';
import './styles/base.css';
import './styles/controls.css';
import App from './App.svelte';

const target = document.getElementById('app');
if (!target) {
  throw new Error('podium: #app element is missing from index.html');
}

// index.html carries a fallback <main> for readers without JavaScript; the
// app brings its own.
target.replaceChildren();

export default mount(App, { target });
