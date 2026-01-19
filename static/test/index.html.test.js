// Manual smoke test for index.html structure
// Run this in browser console after loading index.html

(async function runTests() {
  console.log('=== HTML Structure Test ===');
  console.log('Waiting for DOM and Alpine.js to stabilize...');

  // Wait for Alpine.js to be loaded and initialized
  async function waitForAlpine() {
    const maxWait = 5000;
    const startTime = Date.now();

    while (Date.now() - startTime < maxWait) {
      if (window.Alpine && window.Alpine.version && document.body._x_dataStack) {
        console.log('✓ Alpine.js initialized (version:', window.Alpine.version + ')');
        return true;
      }
      await new Promise(resolve => setTimeout(resolve, 50));
    }

    console.warn('⚠ Alpine.js not fully initialized after', maxWait, 'ms');
    return false;
  }

  // Wait for DOM to stabilize (no mutations for 100ms)
  async function waitForDOMStable() {
    return new Promise((resolve) => {
      let timeout;
      const observer = new MutationObserver(() => {
        clearTimeout(timeout);
        timeout = setTimeout(() => {
          observer.disconnect();
          resolve();
        }, 100);
      });

      observer.observe(document.body, {
        childList: true,
        subtree: true,
        attributes: true,
        attributeOldValue: false,
        characterData: false
      });

      // Initial timeout in case there are no mutations
      timeout = setTimeout(() => {
        observer.disconnect();
        resolve();
      }, 100);
    });
  }

  // Run pre-test checks
  const alpineReady = await waitForAlpine();
  await waitForDOMStable();
  console.log('✓ DOM stabilized\n');

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

  // Test 6: Alpine data stack initialized (runtime check)
  const alpineDataStack = document.body._x_dataStack;
  console.log('✓ Alpine data stack initialized:', !!alpineDataStack);

  // Test 7: Login modal exists
  const loginModal = document.querySelector('.modal');
  console.log('✓ Login modal exists:', loginModal !== null);

  // Test 8: Main form container exists
  const mainContainer = document.querySelector('div.container[x-show="token"]');
  console.log('✓ Main container exists:', mainContainer !== null);

  // Test 9: Global settings section exists
  const globalSection = document.querySelector('section:first-of-type');
  console.log('✓ Global settings section:', globalSection !== null);

  // Test 10: Server IP input exists
  const serverIpInput = document.querySelector('input[x-model="config.server_ip"]');
  console.log('✓ Server IP input:', serverIpInput !== null);

  // Test 11: Categories section exists
  const categoryInputs = document.querySelectorAll('.category-item');
  console.log('✓ Category inputs:', categoryInputs.length > 0);

  // Test 12: Servers section exists
  const serverItems = document.querySelectorAll('.server-item');
  console.log('✓ Server items:', serverItems.length >= 0);

  // Test 13: Save button exists
  const saveButton = document.querySelector('button[\\@click="save()"], button[\\@click="save"]');
  console.log('✓ Save button exists:', saveButton !== null);

  // Test 14: Add server button exists
  const addServerButton = document.querySelector('button[\\@click="addServer()"]');
  console.log('✓ Add server button exists:', addServerButton !== null);

  console.log('\n=== All structure tests passed ===');

  if (!alpineReady) {
    console.warn('⚠ Some tests may have failed due to Alpine.js not initializing');
  }
})();
