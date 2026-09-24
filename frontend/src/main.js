import { mount } from 'svelte';
import '@fontsource-variable/archivo';
import './style.css';
import App from './App.svelte';

mount(App, { target: document.getElementById('app') });
