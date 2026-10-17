<?php
require __DIR__ . '/../../_fixture.php';
$q = fixture('app/ext/curl');
header('Content-Type: text/plain');

switch ($q['op'] ?? '') {
    case 'version':
        $v = curl_version();
        echo preg_match('/^\d+\.\d+\.\d+/', $v['version']) ? 'version ok' : 'bad version', ',',
            in_array('https', $v['protocols'], true) ? 'https' : 'no https';
        break;
    case 'refused':
        // Port 1 on loopback: nothing listens, so connect() fails at once.
        $ch = curl_init('http://127.0.0.1:1/');
        curl_setopt_array($ch, [CURLOPT_RETURNTRANSFER => true, CURLOPT_CONNECTTIMEOUT_MS => 1000, CURLOPT_TIMEOUT_MS => 2000]);
        $r = curl_exec($ch);
        echo var_export($r, true), ',', curl_errno($ch) === CURLE_COULDNT_CONNECT ? 'CURLE_COULDNT_CONNECT' : curl_errno($ch);
        break;
    case 'unsupported':
        $ch = curl_init('nosuchproto://x/');
        curl_setopt($ch, CURLOPT_RETURNTRANSFER, true);
        $r = curl_exec($ch);
        echo var_export($r, true), ',', curl_errno($ch) === CURLE_UNSUPPORTED_PROTOCOL ? 'CURLE_UNSUPPORTED_PROTOCOL' : curl_errno($ch);
        break;
    default:
        http_response_code(400);
        echo 'unknown op';
}
