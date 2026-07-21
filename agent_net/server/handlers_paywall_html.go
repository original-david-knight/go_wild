package server

// checkoutHTML is the self-contained checkout page served at GET /paywall/{product_id}.
const checkoutHTML = `<!DOCTYPE html>
<html lang="en">
<head>
<meta charset="UTF-8">
<meta name="viewport" content="width=device-width, initial-scale=1.0">
<title>Checkout</title>
<style>
  * { margin: 0; padding: 0; box-sizing: border-box; }
  body { font-family: -apple-system, BlinkMacSystemFont, 'Segoe UI', Roboto, sans-serif; background: #0a0a0a; color: #e0e0e0; min-height: 100vh; display: flex; justify-content: center; align-items: center; }
  .card { background: #1a1a2e; border: 1px solid #333; border-radius: 16px; padding: 32px; max-width: 440px; width: 100%; margin: 24px; }
  .card h1 { font-size: 1.5rem; margin-bottom: 8px; color: #fff; }
  .card .description { color: #999; margin-bottom: 24px; line-height: 1.5; }
  .price-row { display: flex; justify-content: space-between; align-items: center; margin-bottom: 24px; padding: 16px; background: #16213e; border-radius: 12px; }
  .price { font-size: 2rem; font-weight: 700; color: #4ade80; }
  .chain-badge { background: #7c3aed; color: #fff; padding: 4px 12px; border-radius: 20px; font-size: 0.85rem; text-transform: uppercase; font-weight: 600; }
  .chain-badge.solana { background: #14f195; color: #000; }
  .file-info { color: #888; font-size: 0.85rem; margin-bottom: 24px; }
  button { width: 100%; padding: 14px; border: none; border-radius: 12px; font-size: 1rem; font-weight: 600; cursor: pointer; transition: opacity 0.2s; }
  button:hover { opacity: 0.9; }
  button:disabled { opacity: 0.5; cursor: not-allowed; }
  #connect-btn { background: #7c3aed; color: #fff; margin-bottom: 12px; }
  #pay-btn { background: #4ade80; color: #000; display: none; margin-bottom: 12px; }
  .status { text-align: center; padding: 12px; border-radius: 8px; margin-top: 12px; display: none; font-size: 0.9rem; }
  .status.error { background: #3b1f1f; color: #f87171; display: block; }
  .status.success { background: #1f3b1f; color: #4ade80; display: block; }
  .status.info { background: #1f2b3b; color: #60a5fa; display: block; }
  .spinner { display: inline-block; width: 16px; height: 16px; border: 2px solid #888; border-top-color: #fff; border-radius: 50%; animation: spin 0.8s linear infinite; vertical-align: middle; margin-right: 8px; }
  @keyframes spin { to { transform: rotate(360deg); } }
  .loading { text-align: center; padding: 48px; color: #888; }
</style>
</head>
<body>
<div class="card" id="card">
  <div class="loading" id="loading">Loading product...</div>
  <div id="product" style="display:none">
    <h1 id="title"></h1>
    <p class="description" id="desc"></p>
    <div class="price-row">
      <span class="price" id="price"></span>
      <span class="chain-badge" id="chain"></span>
    </div>
    <div class="file-info" id="file-info"></div>
    <button id="connect-btn">Connect Wallet</button>
    <button id="pay-btn" data-mode="pay">Pay Now</button>
    <div class="status" id="status"></div>
  </div>
</div>

<script src="https://cdn.jsdelivr.net/npm/ethers@6.13.4/dist/ethers.umd.min.js"></script>
<script>
(function() {
  var productId = window.location.pathname.split('/').pop();
  var infoUrl = '/paywall/' + productId + '/info';
  var verifyUrl = '/paywall/' + productId + '/verify';

  var product = null;
  var walletAddress = null;
  var provider = null;
  var signer = null;
  var lastPaidTxHash = null;
  var lastBuyerSignature = null;

  var $ = function(id) { return document.getElementById(id); };

  function showStatus(msg, type) {
    var el = $('status');
    el.className = 'status ' + type;
    el.innerHTML = msg;
  }

  function hideStatus() {
    $('status').className = 'status';
    $('status').innerHTML = '';
  }

  function formatSize(bytes) {
    if (bytes < 1024) return bytes + ' B';
    if (bytes < 1024 * 1024) return (bytes / 1024).toFixed(1) + ' KB';
    return (bytes / (1024 * 1024)).toFixed(1) + ' MB';
  }

  function buildBuyerProofMessage(txHash) {
    return 'gowild-paywall-verify-v1\nproduct_id:' + productId + '\ntx_hash:' + txHash;
  }

  function bytesToBase64(bytes) {
    var binary = '';
    for (var i = 0; i < bytes.length; i++) {
      binary += String.fromCharCode(bytes[i]);
    }
    return window.btoa(binary);
  }

  function setPayButtonMode(mode) {
    var btn = $('pay-btn');
    btn.setAttribute('data-mode', mode);
    if (mode === 'verify') {
      btn.textContent = 'Retry Verification';
    } else {
      btn.textContent = 'Pay Now';
    }
    btn.disabled = false;
  }

  async function verifyPayment(txHash, buyerSignature) {
    showStatus('<span class="spinner"></span>Verifying payment...', 'info');
    var verifyResp = await fetch(verifyUrl, {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({ tx_hash: txHash, buyer_address: walletAddress, buyer_signature: buyerSignature }),
    });
    var result = await verifyResp.json();
    if (!verifyResp.ok) {
      showStatus('Verification failed: ' + (result.message || result.error || 'Unknown error'), 'error');
      setPayButtonMode('verify');
      return false;
    }
    showStatus('Payment verified! Starting download...', 'success');
    setTimeout(function() {
      window.location.href = '/paywall/dl/' + result.download_token;
    }, 1500);
    return true;
  }

  async function loadProduct() {
    try {
      var resp = await fetch(infoUrl);
      if (!resp.ok) throw new Error('Product not found');
      product = await resp.json();

      $('title').textContent = product.title;
      $('desc').textContent = product.description;
      $('price').textContent = product.price_usdc + ' USDC';
      $('chain').textContent = product.chain;
      if (product.chain === 'solana') $('chain').classList.add('solana');
      $('file-info').textContent = product.file_name + ' (' + formatSize(product.file_size) + ')';

      $('loading').style.display = 'none';
      $('product').style.display = 'block';
    } catch (e) {
      $('loading').textContent = 'Product not found or unavailable.';
    }
  }

  async function connectWallet() {
    hideStatus();
    try {
      if (product.chain === 'polygon') {
        if (!window.ethereum) { showStatus('MetaMask or an EVM wallet is required.', 'error'); return; }
        provider = new ethers.BrowserProvider(window.ethereum);
        var accounts = await provider.send('eth_requestAccounts', []);
        walletAddress = accounts[0];
        try {
          await window.ethereum.request({ method: 'wallet_switchEthereumChain', params: [{ chainId: '0x89' }] });
        } catch (switchErr) {
          if (switchErr.code === 4902) {
            await window.ethereum.request({
              method: 'wallet_addEthereumChain',
              params: [{ chainId: '0x89', chainName: 'Polygon', nativeCurrency: { name: 'MATIC', symbol: 'MATIC', decimals: 18 }, rpcUrls: ['https://polygon-rpc.com'], blockExplorerUrls: ['https://polygonscan.com'] }]
            });
          }
        }
        signer = await provider.getSigner();
      } else if (product.chain === 'solana') {
        if (!window.solana || !window.solana.isPhantom) { showStatus('Phantom wallet is required for Solana.', 'error'); return; }
        var resp = await window.solana.connect();
        walletAddress = resp.publicKey.toString();
      }
      $('connect-btn').textContent = walletAddress.slice(0, 6) + '...' + walletAddress.slice(-4);
      $('connect-btn').disabled = true;
      $('pay-btn').style.display = 'block';
      setPayButtonMode('pay');
    } catch (e) {
      showStatus('Failed to connect wallet: ' + e.message, 'error');
    }
  }

  async function pay() {
    hideStatus();
    var payBtn = $('pay-btn');
    payBtn.disabled = true;
    payBtn.textContent = 'Processing...';

    try {
      if (payBtn.getAttribute('data-mode') === 'verify') {
        if (!lastPaidTxHash || !lastBuyerSignature) {
          showStatus('No prior payment proof found. Please pay again.', 'error');
          setPayButtonMode('pay');
          return;
        }
        await verifyPayment(lastPaidTxHash, lastBuyerSignature);
        return;
      }

      var txHash;
      var buyerSignature;

      if (product.chain === 'polygon') {
        var usdcAddress = '0x3c499c542cEF5E3811e1192ce70d8cC03d5c3359';
        var erc20Abi = ['function transfer(address to, uint256 amount) returns (bool)'];
        var contract = new ethers.Contract(usdcAddress, erc20Abi, signer);
        var amount = ethers.parseUnits(product.price_usdc, 6);
        var tx = await contract.transfer(product.wallet_address, amount);
        showStatus('<span class="spinner"></span>Waiting for confirmation...', 'info');
        var receipt = await tx.wait();
        txHash = receipt.hash;
      } else if (product.chain === 'solana') {
        showStatus('<span class="spinner"></span>Building transaction...', 'info');
        var sol = await import('https://cdn.jsdelivr.net/npm/@solana/web3.js@1.95.3/+esm');
        var connection = new sol.Connection('https://api.mainnet-beta.solana.com', 'confirmed');
        var USDC_MINT = new sol.PublicKey('EPjFWdd5AufqSSqeM2qN1xzybapC8G4wEGGkZwyTDt1v');
        var TOKEN_PROGRAM_ID = new sol.PublicKey('TokenkegQfeZyiNwAJbNbGKPFXCWuBvf9Ss623VQ5DA');
        var ASSOCIATED_TOKEN_PROGRAM_ID = new sol.PublicKey('ATokenGPvbdGVxr1b2hvZbsiqW5xWH25efTNsLJA8knL');
        var fromPubkey = new sol.PublicKey(walletAddress);
        var toPubkey = new sol.PublicKey(product.wallet_address);
        function deriveATA(owner, mint) {
          return sol.PublicKey.findProgramAddressSync(
            [owner.toBuffer(), TOKEN_PROGRAM_ID.toBuffer(), mint.toBuffer()],
            ASSOCIATED_TOKEN_PROGRAM_ID
          )[0];
        }
        var fromATA = deriveATA(fromPubkey, USDC_MINT);
        var toATA = deriveATA(toPubkey, USDC_MINT);
        var amt = BigInt(Math.round(parseFloat(product.price_usdc) * 1e6));
        var dataBuffer = new ArrayBuffer(9);
        var view = new DataView(dataBuffer);
        view.setUint8(0, 3);
        view.setBigUint64(1, amt, true);
        var transferIx = new sol.TransactionInstruction({
          keys: [
            { pubkey: fromATA, isSigner: false, isWritable: true },
            { pubkey: toATA, isSigner: false, isWritable: true },
            { pubkey: fromPubkey, isSigner: true, isWritable: false },
          ],
          programId: TOKEN_PROGRAM_ID,
          data: Buffer.from(new Uint8Array(dataBuffer)),
        });
        var transaction = new sol.Transaction().add(transferIx);
        transaction.feePayer = fromPubkey;
        transaction.recentBlockhash = (await connection.getLatestBlockhash()).blockhash;
        showStatus('<span class="spinner"></span>Approve in wallet...', 'info');
        var signed = await window.solana.signTransaction(transaction);
        var sig = await connection.sendRawTransaction(signed.serialize());
        showStatus('<span class="spinner"></span>Waiting for confirmation...', 'info');
        await connection.confirmTransaction(sig, 'confirmed');
        txHash = sig;
      }

      var proofMessage = buildBuyerProofMessage(txHash);
      showStatus('<span class="spinner"></span>Sign verification proof...', 'info');
      if (product.chain === 'polygon') {
        buyerSignature = await signer.signMessage(proofMessage);
      } else if (product.chain === 'solana') {
        if (!window.solana.signMessage) {
          throw new Error('Connected wallet does not support message signing.');
        }
        var encodedMessage = new TextEncoder().encode(proofMessage);
        var signedMessage = await window.solana.signMessage(encodedMessage, 'utf8');
        buyerSignature = bytesToBase64(signedMessage.signature);
      }

      lastPaidTxHash = txHash;
      lastBuyerSignature = buyerSignature;
      await verifyPayment(txHash, buyerSignature);
    } catch (e) {
      showStatus('Payment failed: ' + e.message, 'error');
      if (lastPaidTxHash && lastBuyerSignature) {
        setPayButtonMode('verify');
      } else {
        setPayButtonMode('pay');
      }
    }
  }

  $('connect-btn').addEventListener('click', connectWallet);
  $('pay-btn').addEventListener('click', pay);
  loadProduct();
})();
</script>
</body>
</html>`
