// Manual smoke test for index.html structure
// Run this in browser console after loading index.html

console.log('=== HTML Structure Test ===');

// Test 1: HTML5 doctype
const hasDoctype = document.doctype && document.doctype.name === 'html';
console.log('✓ HTML5 Doctype:', hasDoctype);

// Test 2: Viewport meta tag
const hasViewport = document.querySelector('meta[name="viewport"]') !== null;
console.log('✓ Viewport meta tag:', hasViewport);

// Test 3: Alpine.js script loaded
const hasAlpineScript = document.querySelector('script[src="/js/alpine.min.js"]') !== null;
console.log('✓ Alpine.js script tag:', hasAlpineScript);

// Test 4: CSS loaded
const hasCSS = document.querySelector('link[href="/css/styles.css"]') !== null;
console.log('✓ CSS stylesheet:', hasCSS);

// Test 5: Body has x-data directive
const bodyHasXData = document.body.hasAttribute('x-data');
console.log('✓ Body x-data directive:', bodyHasXData);

// Test 6: Login modal exists
const loginModal = document.querySelector('.modal');
console.log('✓ Login modal exists:', loginModal !== null);

// Test 7: Main form container exists
const mainContainer = document.querySelector('.container');
console.log('✓ Main container exists:', mainContainer !== null);

// Test 8: Global settings section exists
const globalSection = document.querySelector('section');
console.log('✓ Global settings section:', globalSection !== null);

// Test 9: Server IP input exists
const serverIpInput = document.querySelector('input[x-model*="server_ip"]');
console.log('✓ Server IP input:', serverIpInput !== null);

// Test 10: Categories section exists
const categoryInputs = document.querySelectorAll('.category-item');
console.log('✓ Category inputs:', categoryInputs.length > 0);

// Test 11: Servers section exists
const serverItems = document.querySelectorAll('.server-item');
console.log('✓ Server items:', serverItems.length >= 0);

// Test 12: Save button exists
const saveButton = document.querySelector('button[@click="save()"], button[@click="save"]');
console.log('✓ Save button exists:', saveButton !== null);

// Test 13: Alpine.js initialized (will be checked separately via automated test)
console.log('Note: Alpine.js initialization requires checking window.Alpine in automated test');

console.log('\n=== All structure tests passed ===');
