import{a as e,c as t,d as n,f as r,g as i,h as a,i as o,l as s,m as c,n as l,o as u,p as d,r as f,s as p,t as m,u as h}from"./index-BcjMOIsl.js";var g={};function _(e){let t=g[e];if(t)return t;t=g[e]=[];for(let e=0;e<128;e++){let n=String.fromCharCode(e);t.push(n)}for(let n=0;n<e.length;n++){let r=e.charCodeAt(n);t[r]=`%`+(`0`+r.toString(16).toUpperCase()).slice(-2)}return t}function v(e,t){typeof t!=`string`&&(t=v.defaultChars);let n=_(t);return e.replace(/(%[a-f0-9]{2})+/gi,function(e){let t=``;for(let r=0,i=e.length;r<i;r+=3){let a=parseInt(e.slice(r+1,r+3),16);if(a<128){t+=n[a];continue}if((a&224)==192&&r+3<i){let n=parseInt(e.slice(r+4,r+6),16);if((n&192)==128){let e=a<<6&1984|n&63;e<128?t+=`��`:t+=String.fromCharCode(e),r+=3;continue}}if((a&240)==224&&r+6<i){let n=parseInt(e.slice(r+4,r+6),16),i=parseInt(e.slice(r+7,r+9),16);if((n&192)==128&&(i&192)==128){let e=a<<12&61440|n<<6&4032|i&63;e<2048||e>=55296&&e<=57343?t+=`���`:t+=String.fromCharCode(e),r+=6;continue}}if((a&248)==240&&r+9<i){let n=parseInt(e.slice(r+4,r+6),16),i=parseInt(e.slice(r+7,r+9),16),o=parseInt(e.slice(r+10,r+12),16);if((n&192)==128&&(i&192)==128&&(o&192)==128){let e=a<<18&1835008|n<<12&258048|i<<6&4032|o&63;e<65536||e>1114111?t+=`����`:(e-=65536,t+=String.fromCharCode(55296+(e>>10),56320+(e&1023))),r+=9;continue}}t+=`�`}return t})}v.defaultChars=`;/?:@&=+$,#`,v.componentChars=``;var y={};function b(e){let t=y[e];if(t)return t;t=y[e]=[];for(let e=0;e<128;e++){let n=String.fromCharCode(e);/^[0-9a-z]$/i.test(n)?t.push(n):t.push(`%`+(`0`+e.toString(16).toUpperCase()).slice(-2))}for(let n=0;n<e.length;n++)t[e.charCodeAt(n)]=e[n];return t}function x(e,t,n){typeof t!=`string`&&(n=t,t=x.defaultChars),n===void 0&&(n=!0);let r=b(t),i=``;for(let t=0,a=e.length;t<a;t++){let o=e.charCodeAt(t);if(n&&o===37&&t+2<a&&/^[0-9a-f]{2}$/i.test(e.slice(t+1,t+3))){i+=e.slice(t,t+3),t+=2;continue}if(o<128){i+=r[o];continue}if(o>=55296&&o<=57343){if(o>=55296&&o<=56319&&t+1<a){let n=e.charCodeAt(t+1);if(n>=56320&&n<=57343){i+=encodeURIComponent(e[t]+e[t+1]),t++;continue}}i+=`%EF%BF%BD`;continue}i+=encodeURIComponent(e[t])}return i}x.defaultChars=`;/?:@&=+$,-_.!~*'()#`,x.componentChars=`-_.!~*'()`;function S(e){let t=``;return t+=e.protocol||``,t+=e.slashes?`//`:``,t+=e.auth?e.auth+`@`:``,e.hostname&&e.hostname.indexOf(`:`)!==-1?t+=`[`+e.hostname+`]`:t+=e.hostname||``,t+=e.port?`:`+e.port:``,t+=e.pathname||``,t+=e.search||``,t+=e.hash||``,t}function C(){this.protocol=null,this.slashes=null,this.auth=null,this.port=null,this.hostname=null,this.hash=null,this.search=null,this.pathname=null}var ee=/^([a-z0-9.+-]+:)/i,te=/:[0-9]*$/,ne=/^(\/\/?(?!\/)[^\?\s]*)(\?[^\s]*)?$/,re=[`%`,`/`,`?`,`;`,`#`,`'`,`{`,`}`,`|`,`\\`,`^`,"`",`<`,`>`,`"`,"`",` `,`\r`,`
`,`	`],ie=[`/`,`?`,`#`],ae=255,oe=/^[+a-z0-9A-Z_-]{0,63}$/,se=/^([+a-z0-9A-Z_-]{0,63})(.*)$/,ce={javascript:!0,"javascript:":!0},le={http:!0,https:!0,ftp:!0,gopher:!0,file:!0,"http:":!0,"https:":!0,"ftp:":!0,"gopher:":!0,"file:":!0};function ue(e,t){if(e&&e instanceof C)return e;let n=new C;return n.parse(e,t),n}C.prototype.parse=function(e,t){let n,r,i,a=e;if(a=a.trim(),!t&&e.split(`#`).length===1){let e=ne.exec(a);if(e)return this.pathname=e[1],e[2]&&(this.search=e[2]),this}let o=ee.exec(a);if(o&&(o=o[0],n=o.toLowerCase(),this.protocol=o,a=a.substr(o.length)),(t||o||a.match(/^\/\/[^@\/]+@[^@\/]+/))&&(i=a.substr(0,2)===`//`,i&&!(o&&ce[o])&&(a=a.substr(2),this.slashes=!0)),!ce[o]&&(i||o&&!le[o])){let e=-1;for(let t=0;t<ie.length;t++)r=a.indexOf(ie[t]),r!==-1&&(e===-1||r<e)&&(e=r);let t,n;n=e===-1?a.lastIndexOf(`@`):a.lastIndexOf(`@`,e),n!==-1&&(t=a.slice(0,n),a=a.slice(n+1),this.auth=t),e=-1;for(let t=0;t<re.length;t++)r=a.indexOf(re[t]),r!==-1&&(e===-1||r<e)&&(e=r);e===-1&&(e=a.length),a[e-1]===`:`&&e--;let i=a.slice(0,e);a=a.slice(e),this.parseHost(i),this.hostname=this.hostname||``;let o=this.hostname[0]===`[`&&this.hostname[this.hostname.length-1]===`]`;if(!o){let e=this.hostname.split(/\./);for(let t=0,n=e.length;t<n;t++){let n=e[t];if(n&&!n.match(oe)){let r=``;for(let e=0,t=n.length;e<t;e++)n.charCodeAt(e)>127?r+=`x`:r+=n[e];if(!r.match(oe)){let r=e.slice(0,t),i=e.slice(t+1),o=n.match(se);o&&(r.push(o[1]),i.unshift(o[2])),i.length&&(a=i.join(`.`)+a),this.hostname=r.join(`.`);break}}}}this.hostname.length>ae&&(this.hostname=``),o&&(this.hostname=this.hostname.substr(1,this.hostname.length-2))}let s=a.indexOf(`#`);s!==-1&&(this.hash=a.substr(s),a=a.slice(0,s));let c=a.indexOf(`?`);return c!==-1&&(this.search=a.substr(c),a=a.slice(0,c)),a&&(this.pathname=a),le[n]&&this.hostname&&!this.pathname&&(this.pathname=``),this},C.prototype.parseHost=function(e){let t=te.exec(e);t&&(t=t[0],t!==`:`&&(this.port=t.substr(1)),e=e.substr(0,e.length-t.length)),e&&(this.hostname=e)};var de=i({decode:()=>v,encode:()=>x,format:()=>S,parse:()=>ue}),fe=/[\0-\uD7FF\uE000-\uFFFF]|[\uD800-\uDBFF][\uDC00-\uDFFF]|[\uD800-\uDBFF](?![\uDC00-\uDFFF])|(?:[^\uD800-\uDBFF]|^)[\uDC00-\uDFFF]/,pe=/[\0-\x1F\x7F-\x9F]/,me=/[\xAD\u0600-\u0605\u061C\u06DD\u070F\u0890\u0891\u08E2\u180E\u200B-\u200F\u202A-\u202E\u2060-\u2064\u2066-\u206F\uFEFF\uFFF9-\uFFFB]|\uD804[\uDCBD\uDCCD]|\uD80D[\uDC30-\uDC3F]|\uD82F[\uDCA0-\uDCA3]|\uD834[\uDD73-\uDD7A]|\uDB40[\uDC01\uDC20-\uDC7F]/,he=/[!-#%-\*,-\/:;\?@\[-\]_\{\}\xA1\xA7\xAB\xB6\xB7\xBB\xBF\u037E\u0387\u055A-\u055F\u0589\u058A\u05BE\u05C0\u05C3\u05C6\u05F3\u05F4\u0609\u060A\u060C\u060D\u061B\u061D-\u061F\u066A-\u066D\u06D4\u0700-\u070D\u07F7-\u07F9\u0830-\u083E\u085E\u0964\u0965\u0970\u09FD\u0A76\u0AF0\u0C77\u0C84\u0DF4\u0E4F\u0E5A\u0E5B\u0F04-\u0F12\u0F14\u0F3A-\u0F3D\u0F85\u0FD0-\u0FD4\u0FD9\u0FDA\u104A-\u104F\u10FB\u1360-\u1368\u1400\u166E\u169B\u169C\u16EB-\u16ED\u1735\u1736\u17D4-\u17D6\u17D8-\u17DA\u1800-\u180A\u1944\u1945\u1A1E\u1A1F\u1AA0-\u1AA6\u1AA8-\u1AAD\u1B5A-\u1B60\u1B7D\u1B7E\u1BFC-\u1BFF\u1C3B-\u1C3F\u1C7E\u1C7F\u1CC0-\u1CC7\u1CD3\u2010-\u2027\u2030-\u2043\u2045-\u2051\u2053-\u205E\u207D\u207E\u208D\u208E\u2308-\u230B\u2329\u232A\u2768-\u2775\u27C5\u27C6\u27E6-\u27EF\u2983-\u2998\u29D8-\u29DB\u29FC\u29FD\u2CF9-\u2CFC\u2CFE\u2CFF\u2D70\u2E00-\u2E2E\u2E30-\u2E4F\u2E52-\u2E5D\u3001-\u3003\u3008-\u3011\u3014-\u301F\u3030\u303D\u30A0\u30FB\uA4FE\uA4FF\uA60D-\uA60F\uA673\uA67E\uA6F2-\uA6F7\uA874-\uA877\uA8CE\uA8CF\uA8F8-\uA8FA\uA8FC\uA92E\uA92F\uA95F\uA9C1-\uA9CD\uA9DE\uA9DF\uAA5C-\uAA5F\uAADE\uAADF\uAAF0\uAAF1\uABEB\uFD3E\uFD3F\uFE10-\uFE19\uFE30-\uFE52\uFE54-\uFE61\uFE63\uFE68\uFE6A\uFE6B\uFF01-\uFF03\uFF05-\uFF0A\uFF0C-\uFF0F\uFF1A\uFF1B\uFF1F\uFF20\uFF3B-\uFF3D\uFF3F\uFF5B\uFF5D\uFF5F-\uFF65]|\uD800[\uDD00-\uDD02\uDF9F\uDFD0]|\uD801\uDD6F|\uD802[\uDC57\uDD1F\uDD3F\uDE50-\uDE58\uDE7F\uDEF0-\uDEF6\uDF39-\uDF3F\uDF99-\uDF9C]|\uD803[\uDEAD\uDF55-\uDF59\uDF86-\uDF89]|\uD804[\uDC47-\uDC4D\uDCBB\uDCBC\uDCBE-\uDCC1\uDD40-\uDD43\uDD74\uDD75\uDDC5-\uDDC8\uDDCD\uDDDB\uDDDD-\uDDDF\uDE38-\uDE3D\uDEA9]|\uD805[\uDC4B-\uDC4F\uDC5A\uDC5B\uDC5D\uDCC6\uDDC1-\uDDD7\uDE41-\uDE43\uDE60-\uDE6C\uDEB9\uDF3C-\uDF3E]|\uD806[\uDC3B\uDD44-\uDD46\uDDE2\uDE3F-\uDE46\uDE9A-\uDE9C\uDE9E-\uDEA2\uDF00-\uDF09]|\uD807[\uDC41-\uDC45\uDC70\uDC71\uDEF7\uDEF8\uDF43-\uDF4F\uDFFF]|\uD809[\uDC70-\uDC74]|\uD80B[\uDFF1\uDFF2]|\uD81A[\uDE6E\uDE6F\uDEF5\uDF37-\uDF3B\uDF44]|\uD81B[\uDE97-\uDE9A\uDFE2]|\uD82F\uDC9F|\uD836[\uDE87-\uDE8B]|\uD83A[\uDD5E\uDD5F]/,ge=/[\$\+<->\^`\|~\xA2-\xA6\xA8\xA9\xAC\xAE-\xB1\xB4\xB8\xD7\xF7\u02C2-\u02C5\u02D2-\u02DF\u02E5-\u02EB\u02ED\u02EF-\u02FF\u0375\u0384\u0385\u03F6\u0482\u058D-\u058F\u0606-\u0608\u060B\u060E\u060F\u06DE\u06E9\u06FD\u06FE\u07F6\u07FE\u07FF\u0888\u09F2\u09F3\u09FA\u09FB\u0AF1\u0B70\u0BF3-\u0BFA\u0C7F\u0D4F\u0D79\u0E3F\u0F01-\u0F03\u0F13\u0F15-\u0F17\u0F1A-\u0F1F\u0F34\u0F36\u0F38\u0FBE-\u0FC5\u0FC7-\u0FCC\u0FCE\u0FCF\u0FD5-\u0FD8\u109E\u109F\u1390-\u1399\u166D\u17DB\u1940\u19DE-\u19FF\u1B61-\u1B6A\u1B74-\u1B7C\u1FBD\u1FBF-\u1FC1\u1FCD-\u1FCF\u1FDD-\u1FDF\u1FED-\u1FEF\u1FFD\u1FFE\u2044\u2052\u207A-\u207C\u208A-\u208C\u20A0-\u20C0\u2100\u2101\u2103-\u2106\u2108\u2109\u2114\u2116-\u2118\u211E-\u2123\u2125\u2127\u2129\u212E\u213A\u213B\u2140-\u2144\u214A-\u214D\u214F\u218A\u218B\u2190-\u2307\u230C-\u2328\u232B-\u2426\u2440-\u244A\u249C-\u24E9\u2500-\u2767\u2794-\u27C4\u27C7-\u27E5\u27F0-\u2982\u2999-\u29D7\u29DC-\u29FB\u29FE-\u2B73\u2B76-\u2B95\u2B97-\u2BFF\u2CE5-\u2CEA\u2E50\u2E51\u2E80-\u2E99\u2E9B-\u2EF3\u2F00-\u2FD5\u2FF0-\u2FFF\u3004\u3012\u3013\u3020\u3036\u3037\u303E\u303F\u309B\u309C\u3190\u3191\u3196-\u319F\u31C0-\u31E3\u31EF\u3200-\u321E\u322A-\u3247\u3250\u3260-\u327F\u328A-\u32B0\u32C0-\u33FF\u4DC0-\u4DFF\uA490-\uA4C6\uA700-\uA716\uA720\uA721\uA789\uA78A\uA828-\uA82B\uA836-\uA839\uAA77-\uAA79\uAB5B\uAB6A\uAB6B\uFB29\uFBB2-\uFBC2\uFD40-\uFD4F\uFDCF\uFDFC-\uFDFF\uFE62\uFE64-\uFE66\uFE69\uFF04\uFF0B\uFF1C-\uFF1E\uFF3E\uFF40\uFF5C\uFF5E\uFFE0-\uFFE6\uFFE8-\uFFEE\uFFFC\uFFFD]|\uD800[\uDD37-\uDD3F\uDD79-\uDD89\uDD8C-\uDD8E\uDD90-\uDD9C\uDDA0\uDDD0-\uDDFC]|\uD802[\uDC77\uDC78\uDEC8]|\uD805\uDF3F|\uD807[\uDFD5-\uDFF1]|\uD81A[\uDF3C-\uDF3F\uDF45]|\uD82F\uDC9C|\uD833[\uDF50-\uDFC3]|\uD834[\uDC00-\uDCF5\uDD00-\uDD26\uDD29-\uDD64\uDD6A-\uDD6C\uDD83\uDD84\uDD8C-\uDDA9\uDDAE-\uDDEA\uDE00-\uDE41\uDE45\uDF00-\uDF56]|\uD835[\uDEC1\uDEDB\uDEFB\uDF15\uDF35\uDF4F\uDF6F\uDF89\uDFA9\uDFC3]|\uD836[\uDC00-\uDDFF\uDE37-\uDE3A\uDE6D-\uDE74\uDE76-\uDE83\uDE85\uDE86]|\uD838[\uDD4F\uDEFF]|\uD83B[\uDCAC\uDCB0\uDD2E\uDEF0\uDEF1]|\uD83C[\uDC00-\uDC2B\uDC30-\uDC93\uDCA0-\uDCAE\uDCB1-\uDCBF\uDCC1-\uDCCF\uDCD1-\uDCF5\uDD0D-\uDDAD\uDDE6-\uDE02\uDE10-\uDE3B\uDE40-\uDE48\uDE50\uDE51\uDE60-\uDE65\uDF00-\uDFFF]|\uD83D[\uDC00-\uDED7\uDEDC-\uDEEC\uDEF0-\uDEFC\uDF00-\uDF76\uDF7B-\uDFD9\uDFE0-\uDFEB\uDFF0]|\uD83E[\uDC00-\uDC0B\uDC10-\uDC47\uDC50-\uDC59\uDC60-\uDC87\uDC90-\uDCAD\uDCB0\uDCB1\uDD00-\uDE53\uDE60-\uDE6D\uDE70-\uDE7C\uDE80-\uDE88\uDE90-\uDEBD\uDEBF-\uDEC5\uDECE-\uDEDB\uDEE0-\uDEE8\uDEF0-\uDEF8\uDF00-\uDF92\uDF94-\uDFCA]/,_e=/[ \xA0\u1680\u2000-\u200A\u2028\u2029\u202F\u205F\u3000]/,ve=i({Any:()=>fe,Cc:()=>pe,Cf:()=>me,P:()=>he,S:()=>ge,Z:()=>_e}),ye=new Uint16Array(`ᵁ<Õıʊҝջאٵ۞ޢߖࠏ੊ઑඡ๭༉༦჊ረዡᐕᒝᓃᓟᔥ\0\0\0\0\0\0ᕫᛍᦍᰒᷝ὾⁠↰⊍⏀⏻⑂⠤⤒ⴈ⹈⿎〖㊺㘹㞬㣾㨨㩱㫠㬮ࠀEMabcfglmnoprstu\\bfms¦³¹ÈÏlig耻Æ䃆P耻&䀦cute耻Á䃁reve;䄂Āiyx}rc耻Â䃂;䐐r;쀀𝔄rave耻À䃀pha;䎑acr;䄀d;橓Āgp¡on;䄄f;쀀𝔸plyFunction;恡ing耻Å䃅Ācs¾Ãr;쀀𝒜ign;扔ilde耻Ã䃃ml耻Ä䃄ЀaceforsuåûþėĜĢħĪĀcrêòkslash;或Ŷöø;櫧ed;挆y;䐑ƀcrtąċĔause;戵noullis;愬a;䎒r;쀀𝔅pf;쀀𝔹eve;䋘còēmpeq;扎܀HOacdefhilorsuōőŖƀƞƢƵƷƺǜȕɳɸɾcy;䐧PY耻©䂩ƀcpyŝŢźute;䄆Ā;iŧŨ拒talDifferentialD;慅leys;愭ȀaeioƉƎƔƘron;䄌dil耻Ç䃇rc;䄈nint;戰ot;䄊ĀdnƧƭilla;䂸terDot;䂷òſi;䎧rcleȀDMPTǇǋǑǖot;抙inus;抖lus;投imes;抗oĀcsǢǸkwiseContourIntegral;戲eCurlyĀDQȃȏoubleQuote;思uote;怙ȀlnpuȞȨɇɕonĀ;eȥȦ户;橴ƀgitȯȶȺruent;扡nt;戯ourIntegral;戮ĀfrɌɎ;愂oduct;成nterClockwiseContourIntegral;戳oss;樯cr;쀀𝒞pĀ;Cʄʅ拓ap;才րDJSZacefiosʠʬʰʴʸˋ˗ˡ˦̳ҍĀ;oŹʥtrahd;椑cy;䐂cy;䐅cy;䐏ƀgrsʿ˄ˇger;怡r;憡hv;櫤Āayː˕ron;䄎;䐔lĀ;t˝˞戇a;䎔r;쀀𝔇Āaf˫̧Ācm˰̢riticalȀADGT̖̜̀̆cute;䂴oŴ̋̍;䋙bleAcute;䋝rave;䁠ilde;䋜ond;拄ferentialD;慆Ѱ̽\0\0\0͔͂\0Ѕf;쀀𝔻ƀ;DE͈͉͍䂨ot;惜qual;扐blèCDLRUVͣͲ΂ϏϢϸontourIntegraìȹoɴ͹\0\0ͻ»͉nArrow;懓Āeo·ΤftƀARTΐΖΡrrow;懐ightArrow;懔eåˊngĀLRΫτeftĀARγιrrow;柸ightArrow;柺ightArrow;柹ightĀATϘϞrrow;懒ee;抨pɁϩ\0\0ϯrrow;懑ownArrow;懕erticalBar;戥ǹABLRTaВЪаўѿͼrrowƀ;BUНОТ憓ar;椓pArrow;懵reve;䌑eft˒к\0ц\0ѐightVector;楐eeVector;楞ectorĀ;Bљњ憽ar;楖ightǔѧ\0ѱeeVector;楟ectorĀ;BѺѻ懁ar;楗eeĀ;A҆҇护rrow;憧ĀctҒҗr;쀀𝒟rok;䄐ࠀNTacdfglmopqstuxҽӀӄӋӞӢӧӮӵԡԯԶՒ՝ՠեG;䅊H耻Ð䃐cute耻É䃉ƀaiyӒӗӜron;䄚rc耻Ê䃊;䐭ot;䄖r;쀀𝔈rave耻È䃈ement;戈ĀapӺӾcr;䄒tyɓԆ\0\0ԒmallSquare;旻erySmallSquare;斫ĀgpԦԪon;䄘f;쀀𝔼silon;䎕uĀaiԼՉlĀ;TՂՃ橵ilde;扂librium;懌Āci՗՚r;愰m;橳a;䎗ml耻Ë䃋Āipժկsts;戃onentialE;慇ʀcfiosօֈ֍ֲ׌y;䐤r;쀀𝔉lledɓ֗\0\0֣mallSquare;旼erySmallSquare;斪Ͱֺ\0ֿ\0\0ׄf;쀀𝔽All;戀riertrf;愱cò׋؀JTabcdfgorstר׬ׯ׺؀ؒؖ؛؝أ٬ٲcy;䐃耻>䀾mmaĀ;d׷׸䎓;䏜reve;䄞ƀeiy؇،ؐdil;䄢rc;䄜;䐓ot;䄠r;쀀𝔊;拙pf;쀀𝔾eater̀EFGLSTصلَٖٛ٦qualĀ;Lؾؿ扥ess;招ullEqual;执reater;檢ess;扷lantEqual;橾ilde;扳cr;쀀𝒢;扫ЀAacfiosuڅڋږڛڞڪھۊRDcy;䐪Āctڐڔek;䋇;䁞irc;䄤r;愌lbertSpace;愋ǰگ\0ڲf;愍izontalLine;攀Āctۃۅòکrok;䄦mpńېۘownHumðįqual;扏܀EJOacdfgmnostuۺ۾܃܇܎ܚܞܡܨ݄ݸދޏޕcy;䐕lig;䄲cy;䐁cute耻Í䃍Āiyܓܘrc耻Î䃎;䐘ot;䄰r;愑rave耻Ì䃌ƀ;apܠܯܿĀcgܴܷr;䄪inaryI;慈lieóϝǴ݉\0ݢĀ;eݍݎ戬Āgrݓݘral;戫section;拂isibleĀCTݬݲomma;恣imes;恢ƀgptݿރވon;䄮f;쀀𝕀a;䎙cr;愐ilde;䄨ǫޚ\0ޞcy;䐆l耻Ï䃏ʀcfosuެ޷޼߂ߐĀiyޱ޵rc;䄴;䐙r;쀀𝔍pf;쀀𝕁ǣ߇\0ߌr;쀀𝒥rcy;䐈kcy;䐄΀HJacfosߤߨ߽߬߱ࠂࠈcy;䐥cy;䐌ppa;䎚Āey߶߻dil;䄶;䐚r;쀀𝔎pf;쀀𝕂cr;쀀𝒦րJTaceflmostࠥࠩࠬࡐࡣ঳সে্਷ੇcy;䐉耻<䀼ʀcmnpr࠷࠼ࡁࡄࡍute;䄹bda;䎛g;柪lacetrf;愒r;憞ƀaeyࡗ࡜ࡡron;䄽dil;䄻;䐛Āfsࡨ॰tԀACDFRTUVarࡾࢩࢱࣦ࣠ࣼयज़ΐ४Ānrࢃ࢏gleBracket;柨rowƀ;BR࢙࢚࢞憐ar;懤ightArrow;懆eiling;挈oǵࢷ\0ࣃbleBracket;柦nǔࣈ\0࣒eeVector;楡ectorĀ;Bࣛࣜ懃ar;楙loor;挊ightĀAV࣯ࣵrrow;憔ector;楎Āerँगeƀ;AVउऊऐ抣rrow;憤ector;楚iangleƀ;BEतथऩ抲ar;槏qual;抴pƀDTVषूौownVector;楑eeVector;楠ectorĀ;Bॖॗ憿ar;楘ectorĀ;B॥०憼ar;楒ightáΜs̀EFGLSTॾঋকঝঢভqualGreater;拚ullEqual;扦reater;扶ess;檡lantEqual;橽ilde;扲r;쀀𝔏Ā;eঽা拘ftarrow;懚idot;䄿ƀnpw৔ਖਛgȀLRlr৞৷ਂਐeftĀAR০৬rrow;柵ightArrow;柷ightArrow;柶eftĀarγਊightáοightáϊf;쀀𝕃erĀLRਢਬeftArrow;憙ightArrow;憘ƀchtਾੀੂòࡌ;憰rok;䅁;扪Ѐacefiosuਗ਼੝੠੷੼અઋ઎p;椅y;䐜Ādl੥੯iumSpace;恟lintrf;愳r;쀀𝔐nusPlus;戓pf;쀀𝕄cò੶;䎜ҀJacefostuણધભીଔଙඑ඗ඞcy;䐊cute;䅃ƀaey઴હાron;䅇dil;䅅;䐝ƀgswે૰଎ativeƀMTV૓૟૨ediumSpace;怋hiĀcn૦૘ë૙eryThiî૙tedĀGL૸ଆreaterGreateòٳessLesóੈLine;䀊r;쀀𝔑ȀBnptଢନଷ଺reak;恠BreakingSpace;䂠f;愕ڀ;CDEGHLNPRSTV୕ୖ୪୼஡௫ఄ౞಄ದ೘ൡඅ櫬Āou୛୤ngruent;扢pCap;扭oubleVerticalBar;戦ƀlqxஃஊ஛ement;戉ualĀ;Tஒஓ扠ilde;쀀≂̸ists;戄reater΀;EFGLSTஶஷ஽௉௓௘௥扯qual;扱ullEqual;쀀≧̸reater;쀀≫̸ess;批lantEqual;쀀⩾̸ilde;扵umpń௲௽ownHump;쀀≎̸qual;쀀≏̸eĀfsఊధtTriangleƀ;BEచఛడ拪ar;쀀⧏̸qual;括s̀;EGLSTవశ఼ౄోౘ扮qual;扰reater;扸ess;쀀≪̸lantEqual;쀀⩽̸ilde;扴estedĀGL౨౹reaterGreater;쀀⪢̸essLess;쀀⪡̸recedesƀ;ESಒಓಛ技qual;쀀⪯̸lantEqual;拠ĀeiಫಹverseElement;戌ghtTriangleƀ;BEೋೌ೒拫ar;쀀⧐̸qual;拭ĀquೝഌuareSuĀbp೨೹setĀ;E೰ೳ쀀⊏̸qual;拢ersetĀ;Eഃആ쀀⊐̸qual;拣ƀbcpഓതൎsetĀ;Eഛഞ쀀⊂⃒qual;抈ceedsȀ;ESTലള഻െ抁qual;쀀⪰̸lantEqual;拡ilde;쀀≿̸ersetĀ;E൘൛쀀⊃⃒qual;抉ildeȀ;EFT൮൯൵ൿ扁qual;扄ullEqual;扇ilde;扉erticalBar;戤cr;쀀𝒩ilde耻Ñ䃑;䎝܀Eacdfgmoprstuvලෂ෉෕ෛ෠෧෼ขภยา฿ไlig;䅒cute耻Ó䃓Āiy෎ීrc耻Ô䃔;䐞blac;䅐r;쀀𝔒rave耻Ò䃒ƀaei෮ෲ෶cr;䅌ga;䎩cron;䎟pf;쀀𝕆enCurlyĀDQฎบoubleQuote;怜uote;怘;橔Āclวฬr;쀀𝒪ash耻Ø䃘iŬื฼de耻Õ䃕es;樷ml耻Ö䃖erĀBP๋๠Āar๐๓r;怾acĀek๚๜;揞et;掴arenthesis;揜Ҁacfhilors๿ງຊຏຒດຝະ໼rtialD;戂y;䐟r;쀀𝔓i;䎦;䎠usMinus;䂱Āipຢອncareplanåڝf;愙Ȁ;eio຺ູ໠໤檻cedesȀ;EST່້໏໚扺qual;檯lantEqual;扼ilde;找me;怳Ādp໩໮uct;戏ortionĀ;aȥ໹l;戝Āci༁༆r;쀀𝒫;䎨ȀUfos༑༖༛༟OT耻"䀢r;쀀𝔔pf;愚cr;쀀𝒬؀BEacefhiorsu༾གྷཇའཱིྦྷྪྭ႖ႩႴႾarr;椐G耻®䂮ƀcnrཎནབute;䅔g;柫rĀ;tཛྷཝ憠l;椖ƀaeyཧཬཱron;䅘dil;䅖;䐠Ā;vླྀཹ愜erseĀEUྂྙĀlq྇ྎement;戋uilibrium;懋pEquilibrium;楯r»ཹo;䎡ghtЀACDFTUVa࿁࿫࿳ဢဨၛႇϘĀnr࿆࿒gleBracket;柩rowƀ;BL࿜࿝࿡憒ar;懥eftArrow;懄eiling;按oǵ࿹\0စbleBracket;柧nǔည\0နeeVector;楝ectorĀ;Bဝသ懂ar;楕loor;挋Āerိ၃eƀ;AVဵံြ抢rrow;憦ector;楛iangleƀ;BEၐၑၕ抳ar;槐qual;抵pƀDTVၣၮၸownVector;楏eeVector;楜ectorĀ;Bႂႃ憾ar;楔ectorĀ;B႑႒懀ar;楓Āpuႛ႞f;愝ndImplies;楰ightarrow;懛ĀchႹႼr;愛;憱leDelayed;槴ڀHOacfhimoqstuფჱჷჽᄙᄞᅑᅖᅡᅧᆵᆻᆿĀCcჩხHcy;䐩y;䐨FTcy;䐬cute;䅚ʀ;aeiyᄈᄉᄎᄓᄗ檼ron;䅠dil;䅞rc;䅜;䐡r;쀀𝔖ortȀDLRUᄪᄴᄾᅉownArrow»ОeftArrow»࢚ightArrow»࿝pArrow;憑gma;䎣allCircle;战pf;쀀𝕊ɲᅭ\0\0ᅰt;戚areȀ;ISUᅻᅼᆉᆯ斡ntersection;抓uĀbpᆏᆞsetĀ;Eᆗᆘ抏qual;抑ersetĀ;Eᆨᆩ抐qual;抒nion;抔cr;쀀𝒮ar;拆ȀbcmpᇈᇛሉላĀ;sᇍᇎ拐etĀ;Eᇍᇕqual;抆ĀchᇠህeedsȀ;ESTᇭᇮᇴᇿ扻qual;檰lantEqual;扽ilde;承Tháྌ;我ƀ;esሒሓሣ拑rsetĀ;Eሜም抃qual;抇et»ሓրHRSacfhiorsሾቄ቉ቕ቞ቱቶኟዂወዑORN耻Þ䃞ADE;愢ĀHc቎ቒcy;䐋y;䐦Ābuቚቜ;䀉;䎤ƀaeyብቪቯron;䅤dil;䅢;䐢r;쀀𝔗Āeiቻ኉ǲኀ\0ኇefore;戴a;䎘Ācn኎ኘkSpace;쀀  Space;怉ldeȀ;EFTካኬኲኼ戼qual;扃ullEqual;扅ilde;扈pf;쀀𝕋ipleDot;惛Āctዖዛr;쀀𝒯rok;䅦ૡዷጎጚጦ\0ጬጱ\0\0\0\0\0ጸጽ፷ᎅ\0᏿ᐄᐊᐐĀcrዻጁute耻Ú䃚rĀ;oጇገ憟cir;楉rǣጓ\0጖y;䐎ve;䅬Āiyጞጣrc耻Û䃛;䐣blac;䅰r;쀀𝔘rave耻Ù䃙acr;䅪Ādiፁ፩erĀBPፈ፝Āarፍፐr;䁟acĀekፗፙ;揟et;掵arenthesis;揝onĀ;P፰፱拃lus;抎Āgp፻፿on;䅲f;쀀𝕌ЀADETadps᎕ᎮᎸᏄϨᏒᏗᏳrrowƀ;BDᅐᎠᎤar;椒ownArrow;懅ownArrow;憕quilibrium;楮eeĀ;AᏋᏌ报rrow;憥ownáϳerĀLRᏞᏨeftArrow;憖ightArrow;憗iĀ;lᏹᏺ䏒on;䎥ing;䅮cr;쀀𝒰ilde;䅨ml耻Ü䃜ҀDbcdefosvᐧᐬᐰᐳᐾᒅᒊᒐᒖash;披ar;櫫y;䐒ashĀ;lᐻᐼ抩;櫦Āerᑃᑅ;拁ƀbtyᑌᑐᑺar;怖Ā;iᑏᑕcalȀBLSTᑡᑥᑪᑴar;戣ine;䁼eparator;杘ilde;所ThinSpace;怊r;쀀𝔙pf;쀀𝕍cr;쀀𝒱dash;抪ʀcefosᒧᒬᒱᒶᒼirc;䅴dge;拀r;쀀𝔚pf;쀀𝕎cr;쀀𝒲Ȁfiosᓋᓐᓒᓘr;쀀𝔛;䎞pf;쀀𝕏cr;쀀𝒳ҀAIUacfosuᓱᓵᓹᓽᔄᔏᔔᔚᔠcy;䐯cy;䐇cy;䐮cute耻Ý䃝Āiyᔉᔍrc;䅶;䐫r;쀀𝔜pf;쀀𝕐cr;쀀𝒴ml;䅸ЀHacdefosᔵᔹᔿᕋᕏᕝᕠᕤcy;䐖cute;䅹Āayᕄᕉron;䅽;䐗ot;䅻ǲᕔ\0ᕛoWidtè૙a;䎖r;愨pf;愤cr;쀀𝒵௡ᖃᖊᖐ\0ᖰᖶᖿ\0\0\0\0ᗆᗛᗫᙟ᙭\0ᚕ᚛ᚲᚹ\0ᚾcute耻á䃡reve;䄃̀;Ediuyᖜᖝᖡᖣᖨᖭ戾;쀀∾̳;房rc耻â䃢te肻´̆;䐰lig耻æ䃦Ā;r²ᖺ;쀀𝔞rave耻à䃠ĀepᗊᗖĀfpᗏᗔsym;愵èᗓha;䎱ĀapᗟcĀclᗤᗧr;䄁g;樿ɤᗰ\0\0ᘊʀ;adsvᗺᗻᗿᘁᘇ戧nd;橕;橜lope;橘;橚΀;elmrszᘘᘙᘛᘞᘿᙏᙙ戠;榤e»ᘙsdĀ;aᘥᘦ戡ѡᘰᘲᘴᘶᘸᘺᘼᘾ;榨;榩;榪;榫;榬;榭;榮;榯tĀ;vᙅᙆ戟bĀ;dᙌᙍ抾;榝Āptᙔᙗh;戢»¹arr;捼Āgpᙣᙧon;䄅f;쀀𝕒΀;Eaeiop዁ᙻᙽᚂᚄᚇᚊ;橰cir;橯;扊d;手s;䀧roxĀ;e዁ᚒñᚃing耻å䃥ƀctyᚡᚦᚨr;쀀𝒶;䀪mpĀ;e዁ᚯñʈilde耻ã䃣ml耻ä䃤Āciᛂᛈoninôɲnt;樑ࠀNabcdefiklnoprsu᛭ᛱᜰ᜼ᝃᝈ᝸᝽០៦ᠹᡐᜍ᤽᥈ᥰot;櫭Ācrᛶ᜞kȀcepsᜀᜅᜍᜓong;扌psilon;䏶rime;怵imĀ;e᜚᜛戽q;拍Ŷᜢᜦee;抽edĀ;gᜬᜭ挅e»ᜭrkĀ;t፜᜷brk;掶Āoyᜁᝁ;䐱quo;怞ʀcmprtᝓ᝛ᝡᝤᝨausĀ;eĊĉptyv;榰séᜌnoõēƀahwᝯ᝱ᝳ;䎲;愶een;扬r;쀀𝔟g΀costuvwឍឝឳេ៕៛៞ƀaiuបពរðݠrc;旯p»፱ƀdptឤឨឭot;樀lus;樁imes;樂ɱឹ\0\0ើcup;樆ar;昅riangleĀdu៍្own;施p;斳plus;樄eåᑄåᒭarow;植ƀako៭ᠦᠵĀcn៲ᠣkƀlst៺֫᠂ozenge;槫riangleȀ;dlr᠒᠓᠘᠝斴own;斾eft;旂ight;斸k;搣Ʊᠫ\0ᠳƲᠯ\0ᠱ;斒;斑4;斓ck;斈ĀeoᠾᡍĀ;qᡃᡆ쀀=⃥uiv;쀀≡⃥t;挐Ȁptwxᡙᡞᡧᡬf;쀀𝕓Ā;tᏋᡣom»Ꮜtie;拈؀DHUVbdhmptuvᢅᢖᢪᢻᣗᣛᣬ᣿ᤅᤊᤐᤡȀLRlrᢎᢐᢒᢔ;敗;敔;敖;敓ʀ;DUduᢡᢢᢤᢦᢨ敐;敦;敩;敤;敧ȀLRlrᢳᢵᢷᢹ;敝;敚;敜;教΀;HLRhlrᣊᣋᣍᣏᣑᣓᣕ救;敬;散;敠;敫;敢;敟ox;槉ȀLRlrᣤᣦᣨᣪ;敕;敒;攐;攌ʀ;DUduڽ᣷᣹᣻᣽;敥;敨;攬;攴inus;抟lus;択imes;抠ȀLRlrᤙᤛᤝ᤟;敛;敘;攘;攔΀;HLRhlrᤰᤱᤳᤵᤷ᤻᤹攂;敪;敡;敞;攼;攤;攜Āevģ᥂bar耻¦䂦Ȁceioᥑᥖᥚᥠr;쀀𝒷mi;恏mĀ;e᜚᜜lƀ;bhᥨᥩᥫ䁜;槅sub;柈Ŭᥴ᥾lĀ;e᥹᥺怢t»᥺pƀ;Eeįᦅᦇ;檮Ā;qۜۛೡᦧ\0᧨ᨑᨕᨲ\0ᨷᩐ\0\0᪴\0\0᫁\0\0ᬡᬮ᭍᭒\0᯽\0ᰌƀcpr᦭ᦲ᧝ute;䄇̀;abcdsᦿᧀᧄ᧊᧕᧙戩nd;橄rcup;橉Āau᧏᧒p;橋p;橇ot;橀;쀀∩︀Āeo᧢᧥t;恁îړȀaeiu᧰᧻ᨁᨅǰ᧵\0᧸s;橍on;䄍dil耻ç䃧rc;䄉psĀ;sᨌᨍ橌m;橐ot;䄋ƀdmnᨛᨠᨦil肻¸ƭptyv;榲t脀¢;eᨭᨮ䂢räƲr;쀀𝔠ƀceiᨽᩀᩍy;䑇ckĀ;mᩇᩈ朓ark»ᩈ;䏇r΀;Ecefms᩟᩠ᩢᩫ᪤᪪᪮旋;槃ƀ;elᩩᩪᩭ䋆q;扗eɡᩴ\0\0᪈rrowĀlr᩼᪁eft;憺ight;憻ʀRSacd᪒᪔᪖᪚᪟»ཇ;擈st;抛irc;抚ash;抝nint;樐id;櫯cir;槂ubsĀ;u᪻᪼晣it»᪼ˬ᫇᫔᫺\0ᬊonĀ;eᫍᫎ䀺Ā;qÇÆɭ᫙\0\0᫢aĀ;t᫞᫟䀬;䁀ƀ;fl᫨᫩᫫戁îᅠeĀmx᫱᫶ent»᫩eóɍǧ᫾\0ᬇĀ;dኻᬂot;橭nôɆƀfryᬐᬔᬗ;쀀𝕔oäɔ脀©;sŕᬝr;愗Āaoᬥᬩrr;憵ss;朗Ācuᬲᬷr;쀀𝒸Ābpᬼ᭄Ā;eᭁᭂ櫏;櫑Ā;eᭉᭊ櫐;櫒dot;拯΀delprvw᭠᭬᭷ᮂᮬᯔ᯹arrĀlr᭨᭪;椸;椵ɰ᭲\0\0᭵r;拞c;拟arrĀ;p᭿ᮀ憶;椽̀;bcdosᮏᮐᮖᮡᮥᮨ截rcap;橈Āauᮛᮞp;橆p;橊ot;抍r;橅;쀀∪︀Ȁalrv᮵ᮿᯞᯣrrĀ;mᮼᮽ憷;椼yƀevwᯇᯔᯘqɰᯎ\0\0ᯒreã᭳uã᭵ee;拎edge;拏en耻¤䂤earrowĀlrᯮ᯳eft»ᮀight»ᮽeäᯝĀciᰁᰇoninôǷnt;戱lcty;挭ঀAHabcdefhijlorstuwz᰸᰻᰿ᱝᱩᱵᲊᲞᲬᲷ᳻᳿ᴍᵻᶑᶫᶻ᷆᷍rò΁ar;楥Ȁglrs᱈ᱍ᱒᱔ger;怠eth;愸òᄳhĀ;vᱚᱛ怐»ऊūᱡᱧarow;椏aã̕Āayᱮᱳron;䄏;䐴ƀ;ao̲ᱼᲄĀgrʿᲁr;懊tseq;橷ƀglmᲑᲔᲘ耻°䂰ta;䎴ptyv;榱ĀirᲣᲨsht;楿;쀀𝔡arĀlrᲳᲵ»ࣜ»သʀaegsv᳂͸᳖᳜᳠mƀ;oș᳊᳔ndĀ;ș᳑uit;晦amma;䏝in;拲ƀ;io᳧᳨᳸䃷de脀÷;o᳧ᳰntimes;拇nø᳷cy;䑒cɯᴆ\0\0ᴊrn;挞op;挍ʀlptuwᴘᴝᴢᵉᵕlar;䀤f;쀀𝕕ʀ;emps̋ᴭᴷᴽᵂqĀ;d͒ᴳot;扑inus;戸lus;戔quare;抡blebarwedgåúnƀadhᄮᵝᵧownarrowóᲃarpoonĀlrᵲᵶefôᲴighôᲶŢᵿᶅkaro÷གɯᶊ\0\0ᶎrn;挟op;挌ƀcotᶘᶣᶦĀryᶝᶡ;쀀𝒹;䑕l;槶rok;䄑Ādrᶰᶴot;拱iĀ;fᶺ᠖斿Āah᷀᷃ròЩaòྦangle;榦Āci᷒ᷕy;䑟grarr;柿ऀDacdefglmnopqrstuxḁḉḙḸոḼṉṡṾấắẽỡἪἷὄ὎὚ĀDoḆᴴoôᲉĀcsḎḔute耻é䃩ter;橮ȀaioyḢḧḱḶron;䄛rĀ;cḭḮ扖耻ê䃪lon;払;䑍ot;䄗ĀDrṁṅot;扒;쀀𝔢ƀ;rsṐṑṗ檚ave耻è䃨Ā;dṜṝ檖ot;檘Ȁ;ilsṪṫṲṴ檙nters;揧;愓Ā;dṹṺ檕ot;檗ƀapsẅẉẗcr;䄓tyƀ;svẒẓẕ戅et»ẓpĀ1;ẝẤĳạả;怄;怅怃ĀgsẪẬ;䅋p;怂ĀgpẴẸon;䄙f;쀀𝕖ƀalsỄỎỒrĀ;sỊị拕l;槣us;橱iƀ;lvỚớở䎵on»ớ;䏵ȀcsuvỪỳἋἣĀioữḱrc»Ḯɩỹ\0\0ỻíՈantĀglἂἆtr»ṝess»Ṻƀaeiἒ἖Ἒls;䀽st;扟vĀ;DȵἠD;橸parsl;槥ĀDaἯἳot;打rr;楱ƀcdiἾὁỸr;愯oô͒ĀahὉὋ;䎷耻ð䃰Āmrὓὗl耻ë䃫o;悬ƀcipὡὤὧl;䀡sôծĀeoὬὴctatioîՙnentialåչৡᾒ\0ᾞ\0ᾡᾧ\0\0ῆῌ\0ΐ\0ῦῪ \0 ⁚llingdotseñṄy;䑄male;晀ƀilrᾭᾳ῁lig;耀ﬃɩᾹ\0\0᾽g;耀ﬀig;耀ﬄ;쀀𝔣lig;耀ﬁlig;쀀fjƀaltῙ῜ῡt;晭ig;耀ﬂns;斱of;䆒ǰ΅\0ῳf;쀀𝕗ĀakֿῷĀ;vῼ´拔;櫙artint;樍Āao‌⁕Ācs‑⁒α‚‰‸⁅⁈\0⁐β•‥‧‪‬\0‮耻½䂽;慓耻¼䂼;慕;慙;慛Ƴ‴\0‶;慔;慖ʴ‾⁁\0\0⁃耻¾䂾;慗;慜5;慘ƶ⁌\0⁎;慚;慝8;慞l;恄wn;挢cr;쀀𝒻ࢀEabcdefgijlnorstv₂₉₟₥₰₴⃰⃵⃺⃿℃ℒℸ̗ℾ⅒↞Ā;lٍ₇;檌ƀcmpₐₕ₝ute;䇵maĀ;dₜ᳚䎳;檆reve;䄟Āiy₪₮rc;䄝;䐳ot;䄡Ȁ;lqsؾق₽⃉ƀ;qsؾٌ⃄lanô٥Ȁ;cdl٥⃒⃥⃕c;檩otĀ;o⃜⃝檀Ā;l⃢⃣檂;檄Ā;e⃪⃭쀀⋛︀s;檔r;쀀𝔤Ā;gٳ؛mel;愷cy;䑓Ȁ;Eajٚℌℎℐ;檒;檥;檤ȀEaesℛℝ℩ℴ;扩pĀ;p℣ℤ檊rox»ℤĀ;q℮ℯ檈Ā;q℮ℛim;拧pf;쀀𝕘Āci⅃ⅆr;愊mƀ;el٫ⅎ⅐;檎;檐茀>;cdlqr׮ⅠⅪⅮⅳⅹĀciⅥⅧ;檧r;橺ot;拗Par;榕uest;橼ʀadelsↄⅪ←ٖ↛ǰ↉\0↎proø₞r;楸qĀlqؿ↖lesó₈ií٫Āen↣↭rtneqq;쀀≩︀Å↪ԀAabcefkosy⇄⇇⇱⇵⇺∘∝∯≨≽ròΠȀilmr⇐⇔⇗⇛rsðᒄf»․ilôکĀdr⇠⇤cy;䑊ƀ;cwࣴ⇫⇯ir;楈;憭ar;意irc;䄥ƀalr∁∎∓rtsĀ;u∉∊晥it»∊lip;怦con;抹r;쀀𝔥sĀew∣∩arow;椥arow;椦ʀamopr∺∾≃≞≣rr;懿tht;戻kĀlr≉≓eftarrow;憩ightarrow;憪f;쀀𝕙bar;怕ƀclt≯≴≸r;쀀𝒽asè⇴rok;䄧Ābp⊂⊇ull;恃hen»ᱛૡ⊣\0⊪\0⊸⋅⋎\0⋕⋳\0\0⋸⌢⍧⍢⍿\0⎆⎪⎴cute耻í䃭ƀ;iyݱ⊰⊵rc耻î䃮;䐸Ācx⊼⊿y;䐵cl耻¡䂡ĀfrΟ⋉;쀀𝔦rave耻ì䃬Ȁ;inoܾ⋝⋩⋮Āin⋢⋦nt;樌t;戭fin;槜ta;愩lig;䄳ƀaop⋾⌚⌝ƀcgt⌅⌈⌗r;䄫ƀelpܟ⌏⌓inåގarôܠh;䄱f;抷ed;䆵ʀ;cfotӴ⌬⌱⌽⍁are;愅inĀ;t⌸⌹戞ie;槝doô⌙ʀ;celpݗ⍌⍐⍛⍡al;抺Āgr⍕⍙eróᕣã⍍arhk;樗rod;樼Ȁcgpt⍯⍲⍶⍻y;䑑on;䄯f;쀀𝕚a;䎹uest耻¿䂿Āci⎊⎏r;쀀𝒾nʀ;EdsvӴ⎛⎝⎡ӳ;拹ot;拵Ā;v⎦⎧拴;拳Ā;iݷ⎮lde;䄩ǫ⎸\0⎼cy;䑖l耻ï䃯̀cfmosu⏌⏗⏜⏡⏧⏵Āiy⏑⏕rc;䄵;䐹r;쀀𝔧ath;䈷pf;쀀𝕛ǣ⏬\0⏱r;쀀𝒿rcy;䑘kcy;䑔Ѐacfghjos␋␖␢␧␭␱␵␻ppaĀ;v␓␔䎺;䏰Āey␛␠dil;䄷;䐺r;쀀𝔨reen;䄸cy;䑅cy;䑜pf;쀀𝕜cr;쀀𝓀஀ABEHabcdefghjlmnoprstuv⑰⒁⒆⒍⒑┎┽╚▀♎♞♥♹♽⚚⚲⛘❝❨➋⟀⠁⠒ƀart⑷⑺⑼rò৆òΕail;椛arr;椎Ā;gঔ⒋;檋ar;楢ॣ⒥\0⒪\0⒱\0\0\0\0\0⒵Ⓔ\0ⓆⓈⓍ\0⓹ute;䄺mptyv;榴raîࡌbda;䎻gƀ;dlࢎⓁⓃ;榑åࢎ;檅uo耻«䂫rЀ;bfhlpst࢙ⓞⓦⓩ⓫⓮⓱⓵Ā;f࢝ⓣs;椟s;椝ë≒p;憫l;椹im;楳l;憢ƀ;ae⓿─┄檫il;椙Ā;s┉┊檭;쀀⪭︀ƀabr┕┙┝rr;椌rk;杲Āak┢┬cĀek┨┪;䁻;䁛Āes┱┳;榋lĀdu┹┻;榏;榍Ȁaeuy╆╋╖╘ron;䄾Ādi═╔il;䄼ìࢰâ┩;䐻Ȁcqrs╣╦╭╽a;椶uoĀ;rนᝆĀdu╲╷har;楧shar;楋h;憲ʀ;fgqs▋▌উ◳◿扤tʀahlrt▘▤▷◂◨rrowĀ;t࢙□aé⓶arpoonĀdu▯▴own»њp»०eftarrows;懇ightƀahs◍◖◞rrowĀ;sࣴࢧarpoonó྘quigarro÷⇰hreetimes;拋ƀ;qs▋ও◺lanôবʀ;cdgsব☊☍☝☨c;檨otĀ;o☔☕橿Ā;r☚☛檁;檃Ā;e☢☥쀀⋚︀s;檓ʀadegs☳☹☽♉♋pproøⓆot;拖qĀgq♃♅ôউgtò⒌ôছiíলƀilr♕࣡♚sht;楼;쀀𝔩Ā;Eজ♣;檑š♩♶rĀdu▲♮Ā;l॥♳;楪lk;斄cy;䑙ʀ;achtੈ⚈⚋⚑⚖rò◁orneòᴈard;楫ri;旺Āio⚟⚤dot;䅀ustĀ;a⚬⚭掰che»⚭ȀEaes⚻⚽⛉⛔;扨pĀ;p⛃⛄檉rox»⛄Ā;q⛎⛏檇Ā;q⛎⚻im;拦Ѐabnoptwz⛩⛴⛷✚✯❁❇❐Ānr⛮⛱g;柬r;懽rëࣁgƀlmr⛿✍✔eftĀar০✇ightá৲apsto;柼ightá৽parrowĀlr✥✩efô⓭ight;憬ƀafl✶✹✽r;榅;쀀𝕝us;樭imes;樴š❋❏st;戗áፎƀ;ef❗❘᠀旊nge»❘arĀ;l❤❥䀨t;榓ʀachmt❳❶❼➅➇ròࢨorneòᶌarĀ;d྘➃;業;怎ri;抿̀achiqt➘➝ੀ➢➮➻quo;怹r;쀀𝓁mƀ;egল➪➬;檍;檏Ābu┪➳oĀ;rฟ➹;怚rok;䅂萀<;cdhilqrࠫ⟒☹⟜⟠⟥⟪⟰Āci⟗⟙;檦r;橹reå◲mes;拉arr;楶uest;橻ĀPi⟵⟹ar;榖ƀ;ef⠀भ᠛旃rĀdu⠇⠍shar;楊har;楦Āen⠗⠡rtneqq;쀀≨︀Å⠞܀Dacdefhilnopsu⡀⡅⢂⢎⢓⢠⢥⢨⣚⣢⣤ઃ⣳⤂Dot;戺Ȁclpr⡎⡒⡣⡽r耻¯䂯Āet⡗⡙;時Ā;e⡞⡟朠se»⡟Ā;sျ⡨toȀ;dluျ⡳⡷⡻owîҌefôएðᏑker;斮Āoy⢇⢌mma;権;䐼ash;怔asuredangle»ᘦr;쀀𝔪o;愧ƀcdn⢯⢴⣉ro耻µ䂵Ȁ;acdᑤ⢽⣀⣄sôᚧir;櫰ot肻·Ƶusƀ;bd⣒ᤃ⣓戒Ā;uᴼ⣘;横ţ⣞⣡p;櫛ò−ðઁĀdp⣩⣮els;抧f;쀀𝕞Āct⣸⣽r;쀀𝓂pos»ᖝƀ;lm⤉⤊⤍䎼timap;抸ఀGLRVabcdefghijlmoprstuvw⥂⥓⥾⦉⦘⧚⧩⨕⨚⩘⩝⪃⪕⪤⪨⬄⬇⭄⭿⮮ⰴⱧⱼ⳩Āgt⥇⥋;쀀⋙̸Ā;v⥐௏쀀≫⃒ƀelt⥚⥲⥶ftĀar⥡⥧rrow;懍ightarrow;懎;쀀⋘̸Ā;v⥻ే쀀≪⃒ightarrow;懏ĀDd⦎⦓ash;抯ash;抮ʀbcnpt⦣⦧⦬⦱⧌la»˞ute;䅄g;쀀∠⃒ʀ;Eiop඄⦼⧀⧅⧈;쀀⩰̸d;쀀≋̸s;䅉roø඄urĀ;a⧓⧔普lĀ;s⧓ସǳ⧟\0⧣p肻\xA0ଷmpĀ;e௹ఀʀaeouy⧴⧾⨃⨐⨓ǰ⧹\0⧻;橃on;䅈dil;䅆ngĀ;dൾ⨊ot;쀀⩭̸p;橂;䐽ash;怓΀;Aadqsxஒ⨩⨭⨻⩁⩅⩐rr;懗rĀhr⨳⨶k;椤Ā;oᏲᏰot;쀀≐̸uiöୣĀei⩊⩎ar;椨í஘istĀ;s஠டr;쀀𝔫ȀEest௅⩦⩹⩼ƀ;qs஼⩭௡ƀ;qs஼௅⩴lanô௢ií௪Ā;rஶ⪁»ஷƀAap⪊⪍⪑rò⥱rr;憮ar;櫲ƀ;svྍ⪜ྌĀ;d⪡⪢拼;拺cy;䑚΀AEadest⪷⪺⪾⫂⫅⫶⫹rò⥦;쀀≦̸rr;憚r;急Ȁ;fqs఻⫎⫣⫯tĀar⫔⫙rro÷⫁ightarro÷⪐ƀ;qs఻⪺⫪lanôౕĀ;sౕ⫴»శiíౝĀ;rవ⫾iĀ;eచథiäඐĀpt⬌⬑f;쀀𝕟膀¬;in⬙⬚⬶䂬nȀ;Edvஉ⬤⬨⬮;쀀⋹̸ot;쀀⋵̸ǡஉ⬳⬵;拷;拶iĀ;vಸ⬼ǡಸ⭁⭃;拾;拽ƀaor⭋⭣⭩rȀ;ast୻⭕⭚⭟lleì୻l;쀀⫽⃥;쀀∂̸lint;樔ƀ;ceಒ⭰⭳uåಥĀ;cಘ⭸Ā;eಒ⭽ñಘȀAait⮈⮋⮝⮧rò⦈rrƀ;cw⮔⮕⮙憛;쀀⤳̸;쀀↝̸ghtarrow»⮕riĀ;eೋೖ΀chimpqu⮽⯍⯙⬄୸⯤⯯Ȁ;cerല⯆ഷ⯉uå൅;쀀𝓃ortɭ⬅\0\0⯖ará⭖mĀ;e൮⯟Ā;q൴൳suĀbp⯫⯭å೸åഋƀbcp⯶ⰑⰙȀ;Ees⯿ⰀഢⰄ抄;쀀⫅̸etĀ;eഛⰋqĀ;qണⰀcĀ;eലⰗñസȀ;EesⰢⰣൟⰧ抅;쀀⫆̸etĀ;e൘ⰮqĀ;qൠⰣȀgilrⰽⰿⱅⱇìௗlde耻ñ䃱çృiangleĀlrⱒⱜeftĀ;eచⱚñదightĀ;eೋⱥñ೗Ā;mⱬⱭ䎽ƀ;esⱴⱵⱹ䀣ro;愖p;怇ҀDHadgilrsⲏⲔⲙⲞⲣⲰⲶⳓⳣash;抭arr;椄p;쀀≍⃒ash;抬ĀetⲨⲬ;쀀≥⃒;쀀>⃒nfin;槞ƀAetⲽⳁⳅrr;椂;쀀≤⃒Ā;rⳊⳍ쀀<⃒ie;쀀⊴⃒ĀAtⳘⳜrr;椃rie;쀀⊵⃒im;쀀∼⃒ƀAan⳰⳴ⴂrr;懖rĀhr⳺⳽k;椣Ā;oᏧᏥear;椧ቓ᪕\0\0\0\0\0\0\0\0\0\0\0\0\0ⴭ\0ⴸⵈⵠⵥ⵲ⶄᬇ\0\0ⶍⶫ\0ⷈⷎ\0ⷜ⸙⸫⸾⹃Ācsⴱ᪗ute耻ó䃳ĀiyⴼⵅrĀ;c᪞ⵂ耻ô䃴;䐾ʀabios᪠ⵒⵗǈⵚlac;䅑v;樸old;榼lig;䅓Ācr⵩⵭ir;榿;쀀𝔬ͯ⵹\0\0⵼\0ⶂn;䋛ave耻ò䃲;槁Ābmⶈ෴ar;榵Ȁacitⶕ⶘ⶥⶨrò᪀Āir⶝ⶠr;榾oss;榻nå๒;槀ƀaeiⶱⶵⶹcr;䅍ga;䏉ƀcdnⷀⷅǍron;䎿;榶pf;쀀𝕠ƀaelⷔ⷗ǒr;榷rp;榹΀;adiosvⷪⷫⷮ⸈⸍⸐⸖戨rò᪆Ȁ;efmⷷⷸ⸂⸅橝rĀ;oⷾⷿ愴f»ⷿ耻ª䂪耻º䂺gof;抶r;橖lope;橗;橛ƀclo⸟⸡⸧ò⸁ash耻ø䃸l;折iŬⸯ⸴de耻õ䃵esĀ;aǛ⸺s;樶ml耻ö䃶bar;挽ૡ⹞\0⹽\0⺀⺝\0⺢⺹\0\0⻋ຜ\0⼓\0\0⼫⾼\0⿈rȀ;astЃ⹧⹲຅脀¶;l⹭⹮䂶leìЃɩ⹸\0\0⹻m;櫳;櫽y;䐿rʀcimpt⺋⺏⺓ᡥ⺗nt;䀥od;䀮il;怰enk;怱r;쀀𝔭ƀimo⺨⺰⺴Ā;v⺭⺮䏆;䏕maô੶ne;明ƀ;tv⺿⻀⻈䏀chfork»´;䏖Āau⻏⻟nĀck⻕⻝kĀ;h⇴⻛;愎ö⇴sҀ;abcdemst⻳⻴ᤈ⻹⻽⼄⼆⼊⼎䀫cir;樣ir;樢Āouᵀ⼂;樥;橲n肻±ຝim;樦wo;樧ƀipu⼙⼠⼥ntint;樕f;쀀𝕡nd耻£䂣Ԁ;Eaceinosu່⼿⽁⽄⽇⾁⾉⾒⽾⾶;檳p;檷uå໙Ā;c໎⽌̀;acens່⽙⽟⽦⽨⽾pproø⽃urlyeñ໙ñ໎ƀaes⽯⽶⽺pprox;檹qq;檵im;拨iíໟmeĀ;s⾈ຮ怲ƀEas⽸⾐⽺ð⽵ƀdfp໬⾙⾯ƀals⾠⾥⾪lar;挮ine;挒urf;挓Ā;t໻⾴ï໻rel;抰Āci⿀⿅r;쀀𝓅;䏈ncsp;怈̀fiopsu⿚⋢⿟⿥⿫⿱r;쀀𝔮pf;쀀𝕢rime;恗cr;쀀𝓆ƀaeo⿸〉〓tĀei⿾々rnionóڰnt;樖stĀ;e【】䀿ñἙô༔઀ABHabcdefhilmnoprstux぀けさすムㄎㄫㅇㅢㅲㆎ㈆㈕㈤㈩㉘㉮㉲㊐㊰㊷ƀartぇおがròႳòϝail;検aròᱥar;楤΀cdenqrtとふへみわゔヌĀeuねぱ;쀀∽̱te;䅕iãᅮmptyv;榳gȀ;del࿑らるろ;榒;榥å࿑uo耻»䂻rր;abcfhlpstw࿜ガクシスゼゾダッデナp;極Ā;f࿠ゴs;椠;椳s;椞ë≝ð✮l;楅im;楴l;憣;憝Āaiパフil;椚oĀ;nホボ戶aló༞ƀabrョリヮrò៥rk;杳ĀakンヽcĀekヹ・;䁽;䁝Āes㄂㄄;榌lĀduㄊㄌ;榎;榐Ȁaeuyㄗㄜㄧㄩron;䅙Ādiㄡㄥil;䅗ì࿲âヺ;䑀Ȁclqsㄴㄷㄽㅄa;椷dhar;楩uoĀ;rȎȍh;憳ƀacgㅎㅟངlȀ;ipsླྀㅘㅛႜnåႻarôྩt;断ƀilrㅩဣㅮsht;楽;쀀𝔯ĀaoㅷㆆrĀduㅽㅿ»ѻĀ;l႑ㆄ;楬Ā;vㆋㆌ䏁;䏱ƀgns㆕ㇹㇼht̀ahlrstㆤㆰ㇂㇘㇤㇮rrowĀ;t࿜ㆭaéトarpoonĀduㆻㆿowîㅾp»႒eftĀah㇊㇐rrowó࿪arpoonóՑightarrows;應quigarro÷ニhreetimes;拌g;䋚ingdotseñἲƀahm㈍㈐㈓rò࿪aòՑ;怏oustĀ;a㈞㈟掱che»㈟mid;櫮Ȁabpt㈲㈽㉀㉒Ānr㈷㈺g;柭r;懾rëဃƀafl㉇㉊㉎r;榆;쀀𝕣us;樮imes;樵Āap㉝㉧rĀ;g㉣㉤䀩t;榔olint;樒arò㇣Ȁachq㉻㊀Ⴜ㊅quo;怺r;쀀𝓇Ābu・㊊oĀ;rȔȓƀhir㊗㊛㊠reåㇸmes;拊iȀ;efl㊪ၙᠡ㊫方tri;槎luhar;楨;愞ൡ㋕㋛㋟㌬㌸㍱\0㍺㎤\0\0㏬㏰\0㐨㑈㑚㒭㒱㓊㓱\0㘖\0\0㘳cute;䅛quï➺Ԁ;Eaceinpsyᇭ㋳㋵㋿㌂㌋㌏㌟㌦㌩;檴ǰ㋺\0㋼;檸on;䅡uåᇾĀ;dᇳ㌇il;䅟rc;䅝ƀEas㌖㌘㌛;檶p;檺im;择olint;樓iíሄ;䑁otƀ;be㌴ᵇ㌵担;橦΀Aacmstx㍆㍊㍗㍛㍞㍣㍭rr;懘rĀhr㍐㍒ë∨Ā;oਸ਼਴t耻§䂧i;䀻war;椩mĀin㍩ðnuóñt;朶rĀ;o㍶⁕쀀𝔰Ȁacoy㎂㎆㎑㎠rp;景Āhy㎋㎏cy;䑉;䑈rtɭ㎙\0\0㎜iäᑤaraì⹯耻­䂭Āgm㎨㎴maƀ;fv㎱㎲㎲䏃;䏂Ѐ;deglnprካ㏅㏉㏎㏖㏞㏡㏦ot;橪Ā;q኱ኰĀ;E㏓㏔檞;檠Ā;E㏛㏜檝;檟e;扆lus;樤arr;楲aròᄽȀaeit㏸㐈㐏㐗Āls㏽㐄lsetmé㍪hp;樳parsl;槤Ādlᑣ㐔e;挣Ā;e㐜㐝檪Ā;s㐢㐣檬;쀀⪬︀ƀflp㐮㐳㑂tcy;䑌Ā;b㐸㐹䀯Ā;a㐾㐿槄r;挿f;쀀𝕤aĀdr㑍ЂesĀ;u㑔㑕晠it»㑕ƀcsu㑠㑹㒟Āau㑥㑯pĀ;sᆈ㑫;쀀⊓︀pĀ;sᆴ㑵;쀀⊔︀uĀbp㑿㒏ƀ;esᆗᆜ㒆etĀ;eᆗ㒍ñᆝƀ;esᆨᆭ㒖etĀ;eᆨ㒝ñᆮƀ;afᅻ㒦ְrť㒫ֱ»ᅼaròᅈȀcemt㒹㒾㓂㓅r;쀀𝓈tmîñiì㐕aræᆾĀar㓎㓕rĀ;f㓔ឿ昆Āan㓚㓭ightĀep㓣㓪psiloîỠhé⺯s»⡒ʀbcmnp㓻㕞ሉ㖋㖎Ҁ;Edemnprs㔎㔏㔑㔕㔞㔣㔬㔱㔶抂;櫅ot;檽Ā;dᇚ㔚ot;櫃ult;櫁ĀEe㔨㔪;櫋;把lus;檿arr;楹ƀeiu㔽㕒㕕tƀ;en㔎㕅㕋qĀ;qᇚ㔏eqĀ;q㔫㔨m;櫇Ābp㕚㕜;櫕;櫓c̀;acensᇭ㕬㕲㕹㕻㌦pproø㋺urlyeñᇾñᇳƀaes㖂㖈㌛pproø㌚qñ㌗g;晪ڀ123;Edehlmnps㖩㖬㖯ሜ㖲㖴㗀㗉㗕㗚㗟㗨㗭耻¹䂹耻²䂲耻³䂳;櫆Āos㖹㖼t;檾ub;櫘Ā;dሢ㗅ot;櫄sĀou㗏㗒l;柉b;櫗arr;楻ult;櫂ĀEe㗤㗦;櫌;抋lus;櫀ƀeiu㗴㘉㘌tƀ;enሜ㗼㘂qĀ;qሢ㖲eqĀ;q㗧㗤m;櫈Ābp㘑㘓;櫔;櫖ƀAan㘜㘠㘭rr;懙rĀhr㘦㘨ë∮Ā;oਫ਩war;椪lig耻ß䃟௡㙑㙝㙠ዎ㙳㙹\0㙾㛂\0\0\0\0\0㛛㜃\0㜉㝬\0\0\0㞇ɲ㙖\0\0㙛get;挖;䏄rë๟ƀaey㙦㙫㙰ron;䅥dil;䅣;䑂lrec;挕r;쀀𝔱Ȁeiko㚆㚝㚵㚼ǲ㚋\0㚑eĀ4fኄኁaƀ;sv㚘㚙㚛䎸ym;䏑Ācn㚢㚲kĀas㚨㚮pproø዁im»ኬsðኞĀas㚺㚮ð዁rn耻þ䃾Ǭ̟㛆⋧es膀×;bd㛏㛐㛘䃗Ā;aᤏ㛕r;樱;樰ƀeps㛡㛣㜀á⩍Ȁ;bcf҆㛬㛰㛴ot;挶ir;櫱Ā;o㛹㛼쀀𝕥rk;櫚á㍢rime;怴ƀaip㜏㜒㝤dåቈ΀adempst㜡㝍㝀㝑㝗㝜㝟ngleʀ;dlqr㜰㜱㜶㝀㝂斵own»ᶻeftĀ;e⠀㜾ñम;扜ightĀ;e㊪㝋ñၚot;旬inus;樺lus;樹b;槍ime;樻ezium;揢ƀcht㝲㝽㞁Āry㝷㝻;쀀𝓉;䑆cy;䑛rok;䅧Āio㞋㞎xô᝷headĀlr㞗㞠eftarro÷ࡏightarrow»ཝऀAHabcdfghlmoprstuw㟐㟓㟗㟤㟰㟼㠎㠜㠣㠴㡑㡝㡫㢩㣌㣒㣪㣶ròϭar;楣Ācr㟜㟢ute耻ú䃺òᅐrǣ㟪\0㟭y;䑞ve;䅭Āiy㟵㟺rc耻û䃻;䑃ƀabh㠃㠆㠋ròᎭlac;䅱aòᏃĀir㠓㠘sht;楾;쀀𝔲rave耻ù䃹š㠧㠱rĀlr㠬㠮»ॗ»ႃlk;斀Āct㠹㡍ɯ㠿\0\0㡊rnĀ;e㡅㡆挜r»㡆op;挏ri;旸Āal㡖㡚cr;䅫肻¨͉Āgp㡢㡦on;䅳f;쀀𝕦̀adhlsuᅋ㡸㡽፲㢑㢠ownáᎳarpoonĀlr㢈㢌efô㠭ighô㠯iƀ;hl㢙㢚㢜䏅»ᏺon»㢚parrows;懈ƀcit㢰㣄㣈ɯ㢶\0\0㣁rnĀ;e㢼㢽挝r»㢽op;挎ng;䅯ri;旹cr;쀀𝓊ƀdir㣙㣝㣢ot;拰lde;䅩iĀ;f㜰㣨»᠓Āam㣯㣲rò㢨l耻ü䃼angle;榧ހABDacdeflnoprsz㤜㤟㤩㤭㦵㦸㦽㧟㧤㧨㧳㧹㧽㨁㨠ròϷarĀ;v㤦㤧櫨;櫩asèϡĀnr㤲㤷grt;榜΀eknprst㓣㥆㥋㥒㥝㥤㦖appá␕othinçẖƀhir㓫⻈㥙opô⾵Ā;hᎷ㥢ïㆍĀiu㥩㥭gmá㎳Ābp㥲㦄setneqĀ;q㥽㦀쀀⊊︀;쀀⫋︀setneqĀ;q㦏㦒쀀⊋︀;쀀⫌︀Āhr㦛㦟etá㚜iangleĀlr㦪㦯eft»थight»ၑy;䐲ash»ံƀelr㧄㧒㧗ƀ;beⷪ㧋㧏ar;抻q;扚lip;拮Ābt㧜ᑨaòᑩr;쀀𝔳tré㦮suĀbp㧯㧱»ജ»൙pf;쀀𝕧roð໻tré㦴Ācu㨆㨋r;쀀𝓋Ābp㨐㨘nĀEe㦀㨖»㥾nĀEe㦒㨞»㦐igzag;榚΀cefoprs㨶㨻㩖㩛㩔㩡㩪irc;䅵Ādi㩀㩑Ābg㩅㩉ar;機eĀ;qᗺ㩏;扙erp;愘r;쀀𝔴pf;쀀𝕨Ā;eᑹ㩦atèᑹcr;쀀𝓌ૣណ㪇\0㪋\0㪐㪛\0\0㪝㪨㪫㪯\0\0㫃㫎\0㫘ៜ៟tré៑r;쀀𝔵ĀAa㪔㪗ròσrò৶;䎾ĀAa㪡㪤ròθrò৫að✓is;拻ƀdptឤ㪵㪾Āfl㪺ឩ;쀀𝕩imåឲĀAa㫇㫊ròώròਁĀcq㫒ីr;쀀𝓍Āpt៖㫜ré។Ѐacefiosu㫰㫽㬈㬌㬑㬕㬛㬡cĀuy㫶㫻te耻ý䃽;䑏Āiy㬂㬆rc;䅷;䑋n耻¥䂥r;쀀𝔶cy;䑗pf;쀀𝕪cr;쀀𝓎Ācm㬦㬩y;䑎l耻ÿ䃿Ԁacdefhiosw㭂㭈㭔㭘㭤㭩㭭㭴㭺㮀cute;䅺Āay㭍㭒ron;䅾;䐷ot;䅼Āet㭝㭡træᕟa;䎶r;쀀𝔷cy;䐶grarr;懝pf;쀀𝕫cr;쀀𝓏Ājn㮅㮇;怍j;怌`.split(``).map(e=>e.charCodeAt(0))),be=new Uint16Array(`Ȁaglq	\x1Bɭ\0\0p;䀦os;䀧t;䀾t;䀼uot;䀢`.split(``).map(e=>e.charCodeAt(0))),xe=new Map([[0,65533],[128,8364],[130,8218],[131,402],[132,8222],[133,8230],[134,8224],[135,8225],[136,710],[137,8240],[138,352],[139,8249],[140,338],[142,381],[145,8216],[146,8217],[147,8220],[148,8221],[149,8226],[150,8211],[151,8212],[152,732],[153,8482],[154,353],[155,8250],[156,339],[158,382],[159,376]]),Se=String.fromCodePoint??function(e){let t=``;return e>65535&&(e-=65536,t+=String.fromCharCode(e>>>10&1023|55296),e=56320|e&1023),t+=String.fromCharCode(e),t};function Ce(e){return e>=55296&&e<=57343||e>1114111?65533:xe.get(e)??e}var w;(function(e){e[e.NUM=35]=`NUM`,e[e.SEMI=59]=`SEMI`,e[e.EQUALS=61]=`EQUALS`,e[e.ZERO=48]=`ZERO`,e[e.NINE=57]=`NINE`,e[e.LOWER_A=97]=`LOWER_A`,e[e.LOWER_F=102]=`LOWER_F`,e[e.LOWER_X=120]=`LOWER_X`,e[e.LOWER_Z=122]=`LOWER_Z`,e[e.UPPER_A=65]=`UPPER_A`,e[e.UPPER_F=70]=`UPPER_F`,e[e.UPPER_Z=90]=`UPPER_Z`})(w||={});var we=32,T;(function(e){e[e.VALUE_LENGTH=49152]=`VALUE_LENGTH`,e[e.BRANCH_LENGTH=16256]=`BRANCH_LENGTH`,e[e.JUMP_TABLE=127]=`JUMP_TABLE`})(T||={});function Te(e){return e>=w.ZERO&&e<=w.NINE}function Ee(e){return e>=w.UPPER_A&&e<=w.UPPER_F||e>=w.LOWER_A&&e<=w.LOWER_F}function De(e){return e>=w.UPPER_A&&e<=w.UPPER_Z||e>=w.LOWER_A&&e<=w.LOWER_Z||Te(e)}function Oe(e){return e===w.EQUALS||De(e)}var E;(function(e){e[e.EntityStart=0]=`EntityStart`,e[e.NumericStart=1]=`NumericStart`,e[e.NumericDecimal=2]=`NumericDecimal`,e[e.NumericHex=3]=`NumericHex`,e[e.NamedEntity=4]=`NamedEntity`})(E||={});var D;(function(e){e[e.Legacy=0]=`Legacy`,e[e.Strict=1]=`Strict`,e[e.Attribute=2]=`Attribute`})(D||={});var ke=class{constructor(e,t,n){this.decodeTree=e,this.emitCodePoint=t,this.errors=n,this.state=E.EntityStart,this.consumed=1,this.result=0,this.treeIndex=0,this.excess=1,this.decodeMode=D.Strict}startEntity(e){this.decodeMode=e,this.state=E.EntityStart,this.result=0,this.treeIndex=0,this.excess=1,this.consumed=1}write(e,t){switch(this.state){case E.EntityStart:return e.charCodeAt(t)===w.NUM?(this.state=E.NumericStart,this.consumed+=1,this.stateNumericStart(e,t+1)):(this.state=E.NamedEntity,this.stateNamedEntity(e,t));case E.NumericStart:return this.stateNumericStart(e,t);case E.NumericDecimal:return this.stateNumericDecimal(e,t);case E.NumericHex:return this.stateNumericHex(e,t);case E.NamedEntity:return this.stateNamedEntity(e,t)}}stateNumericStart(e,t){return t>=e.length?-1:(e.charCodeAt(t)|we)===w.LOWER_X?(this.state=E.NumericHex,this.consumed+=1,this.stateNumericHex(e,t+1)):(this.state=E.NumericDecimal,this.stateNumericDecimal(e,t))}addToNumericResult(e,t,n,r){if(t!==n){let i=n-t;this.result=this.result*r**+i+parseInt(e.substr(t,i),r),this.consumed+=i}}stateNumericHex(e,t){let n=t;for(;t<e.length;){let r=e.charCodeAt(t);if(Te(r)||Ee(r))t+=1;else return this.addToNumericResult(e,n,t,16),this.emitNumericEntity(r,3)}return this.addToNumericResult(e,n,t,16),-1}stateNumericDecimal(e,t){let n=t;for(;t<e.length;){let r=e.charCodeAt(t);if(Te(r))t+=1;else return this.addToNumericResult(e,n,t,10),this.emitNumericEntity(r,2)}return this.addToNumericResult(e,n,t,10),-1}emitNumericEntity(e,t){var n;if(this.consumed<=t)return(n=this.errors)==null||n.absenceOfDigitsInNumericCharacterReference(this.consumed),0;if(e===w.SEMI)this.consumed+=1;else if(this.decodeMode===D.Strict)return 0;return this.emitCodePoint(Ce(this.result),this.consumed),this.errors&&(e!==w.SEMI&&this.errors.missingSemicolonAfterCharacterReference(),this.errors.validateNumericCharacterReference(this.result)),this.consumed}stateNamedEntity(e,t){let{decodeTree:n}=this,r=n[this.treeIndex],i=(r&T.VALUE_LENGTH)>>14;for(;t<e.length;t++,this.excess++){let a=e.charCodeAt(t);if(this.treeIndex=je(n,r,this.treeIndex+Math.max(1,i),a),this.treeIndex<0)return this.result===0||this.decodeMode===D.Attribute&&(i===0||Oe(a))?0:this.emitNotTerminatedNamedEntity();if(r=n[this.treeIndex],i=(r&T.VALUE_LENGTH)>>14,i!==0){if(a===w.SEMI)return this.emitNamedEntityData(this.treeIndex,i,this.consumed+this.excess);this.decodeMode!==D.Strict&&(this.result=this.treeIndex,this.consumed+=this.excess,this.excess=0)}}return-1}emitNotTerminatedNamedEntity(){var e;let{result:t,decodeTree:n}=this,r=(n[t]&T.VALUE_LENGTH)>>14;return this.emitNamedEntityData(t,r,this.consumed),(e=this.errors)==null||e.missingSemicolonAfterCharacterReference(),this.consumed}emitNamedEntityData(e,t,n){let{decodeTree:r}=this;return this.emitCodePoint(t===1?r[e]&~T.VALUE_LENGTH:r[e+1],n),t===3&&this.emitCodePoint(r[e+2],n),n}end(){var e;switch(this.state){case E.NamedEntity:return this.result!==0&&(this.decodeMode!==D.Attribute||this.result===this.treeIndex)?this.emitNotTerminatedNamedEntity():0;case E.NumericDecimal:return this.emitNumericEntity(0,2);case E.NumericHex:return this.emitNumericEntity(0,3);case E.NumericStart:return(e=this.errors)==null||e.absenceOfDigitsInNumericCharacterReference(this.consumed),0;case E.EntityStart:return 0}}};function Ae(e){let t=``,n=new ke(e,e=>t+=Se(e));return function(e,r){let i=0,a=0;for(;(a=e.indexOf(`&`,a))>=0;){t+=e.slice(i,a),n.startEntity(r);let o=n.write(e,a+1);if(o<0){i=a+n.end();break}i=a+o,a=o===0?i+1:i}let o=t+e.slice(i);return t=``,o}}function je(e,t,n,r){let i=(t&T.BRANCH_LENGTH)>>7,a=t&T.JUMP_TABLE;if(i===0)return a!==0&&r===a?n:-1;if(a){let t=r-a;return t<0||t>=i?-1:e[n+t]-1}let o=n,s=o+i-1;for(;o<=s;){let t=o+s>>>1,n=e[t];if(n<r)o=t+1;else if(n>r)s=t-1;else return e[t+i]}return-1}var Me=Ae(ye);Ae(be);function Ne(e,t=D.Legacy){return Me(e,t)}function Pe(e){return Me(e,D.Strict)}var Fe=i({arrayReplaceAt:()=>Ve,asciiTrim:()=>I,assign:()=>Be,escapeHtml:()=>A,escapeRE:()=>$e,fromCodePoint:()=>O,has:()=>ze,isMdAsciiPunct:()=>P,isPunctChar:()=>et,isPunctCharCode:()=>N,isSpace:()=>j,isString:()=>Le,isValidEntityCode:()=>He,isWhiteSpace:()=>M,lib:()=>nt,normalizeReference:()=>F,unescapeAll:()=>k,unescapeMd:()=>qe});function Ie(e){return Object.prototype.toString.call(e)}function Le(e){return Ie(e)===`[object String]`}var Re=Object.prototype.hasOwnProperty;function ze(e,t){return Re.call(e,t)}function Be(e){return Array.prototype.slice.call(arguments,1).forEach(function(t){if(t){if(typeof t!=`object`)throw TypeError(t+`must be object`);Object.keys(t).forEach(function(n){e[n]=t[n]})}}),e}function Ve(e,t,n){return[].concat(e.slice(0,t),n,e.slice(t+1))}function He(e){return!(e>=55296&&e<=57343||e>=64976&&e<=65007||(e&65535)==65535||(e&65535)==65534||e>=0&&e<=8||e===11||e>=14&&e<=31||e>=127&&e<=159||e>1114111)}function O(e){if(e>65535){e-=65536;let t=55296+(e>>10),n=56320+(e&1023);return String.fromCharCode(t,n)}return String.fromCharCode(e)}var Ue=/\\([!"#$%&'()*+,\-./:;<=>?@[\\\]^_`{|}~])/g,We=RegExp(Ue.source+`|&([a-z#][a-z0-9]{1,31});`,`gi`),Ge=/^#((?:x[a-f0-9]{1,8}|[0-9]{1,8}))$/i;function Ke(e,t){if(t.charCodeAt(0)===35&&Ge.test(t)){let n=t[1].toLowerCase()===`x`?parseInt(t.slice(2),16):parseInt(t.slice(1),10);return He(n)?O(n):e}let n=Ne(e);return n===e?e:n}function qe(e){return e.indexOf(`\\`)<0?e:e.replace(Ue,`$1`)}function k(e){return e.indexOf(`\\`)<0&&e.indexOf(`&`)<0?e:e.replace(We,function(e,t,n){return t||Ke(e,n)})}var Je=/[&<>"]/,Ye=/[&<>"]/g,Xe={"&":`&amp;`,"<":`&lt;`,">":`&gt;`,'"':`&quot;`};function Ze(e){return Xe[e]}function A(e){return Je.test(e)?e.replace(Ye,Ze):e}var Qe=/[.?*+^$[\]\\(){}|-]/g;function $e(e){return e.replace(Qe,`\\$&`)}function j(e){switch(e){case 9:case 32:return!0}return!1}function M(e){if(e>=8192&&e<=8202)return!0;switch(e){case 9:case 10:case 11:case 12:case 13:case 32:case 160:case 5760:case 8239:case 8287:case 12288:return!0}return!1}function et(e){return he.test(e)||ge.test(e)}function N(e){return et(O(e))}function P(e){switch(e){case 33:case 34:case 35:case 36:case 37:case 38:case 39:case 40:case 41:case 42:case 43:case 44:case 45:case 46:case 47:case 58:case 59:case 60:case 61:case 62:case 63:case 64:case 91:case 92:case 93:case 94:case 95:case 96:case 123:case 124:case 125:case 126:return!0;default:return!1}}function F(e){return e=e.trim().replace(/\s+/g,` `),e.toLowerCase().toUpperCase()}function tt(e){return e===32||e===9||e===10||e===13}function I(e){let t=0;for(;t<e.length&&tt(e.charCodeAt(t));t++);let n=e.length-1;for(;n>=t&&tt(e.charCodeAt(n));n--);return e.slice(t,n+1)}var nt={mdurl:de,ucmicro:ve};function rt(e,t,n){let r,i,a,o,s=e.posMax,c=e.pos;for(e.pos=t+1,r=1;e.pos<s;){if(a=e.src.charCodeAt(e.pos),a===93&&(r--,r===0)){i=!0;break}if(o=e.pos,e.md.inline.skipToken(e),a===91){if(o===e.pos-1)r++;else if(n)return e.pos=c,-1}}let l=-1;return i&&(l=e.pos),e.pos=c,l}function it(e,t,n){let r,i=t,a={ok:!1,pos:0,str:``};if(e.charCodeAt(i)===60){for(i++;i<n;){if(r=e.charCodeAt(i),r===10||r===60)return a;if(r===62)return a.pos=i+1,a.str=k(e.slice(t+1,i)),a.ok=!0,a;if(r===92&&i+1<n){i+=2;continue}i++}return a}let o=0;for(;i<n&&(r=e.charCodeAt(i),!(r===32||r<32||r===127));){if(r===92&&i+1<n){if(e.charCodeAt(i+1)===32)break;i+=2;continue}if(r===40&&(o++,o>32))return a;if(r===41){if(o===0)break;o--}i++}return t===i||o!==0?a:(a.str=k(e.slice(t,i)),a.pos=i,a.ok=!0,a)}function at(e,t,n,r){let i,a=t,o={ok:!1,can_continue:!1,pos:0,str:``,marker:0};if(r)o.str=r.str,o.marker=r.marker;else{if(a>=n)return o;let r=e.charCodeAt(a);if(r!==34&&r!==39&&r!==40)return o;t++,a++,r===40&&(r=41),o.marker=r}for(;a<n;){if(i=e.charCodeAt(a),i===o.marker)return o.pos=a+1,o.str+=k(e.slice(t,a)),o.ok=!0,o;if(i===40&&o.marker===41)return o;i===92&&a+1<n&&a++,a++}return o.can_continue=!0,o.str+=k(e.slice(t,a)),o}var ot=i({parseLinkDestination:()=>it,parseLinkLabel:()=>rt,parseLinkTitle:()=>at}),L={};L.code_inline=function(e,t,n,r,i){let a=e[t];return`<code`+i.renderAttrs(a)+`>`+A(a.content)+`</code>`},L.code_block=function(e,t,n,r,i){let a=e[t];return`<pre`+i.renderAttrs(a)+`><code>`+A(e[t].content)+`</code></pre>
`},L.fence=function(e,t,n,r,i){let a=e[t],o=a.info?k(a.info).trim():``,s=``,c=``;if(o){let e=o.split(/(\s+)/g);s=e[0],c=e.slice(2).join(``)}let l;if(l=n.highlight&&n.highlight(a.content,s,c)||A(a.content),l.indexOf(`<pre`)===0)return l+`
`;if(o){let e=a.attrIndex(`class`),t=a.attrs?a.attrs.slice():[];e<0?t.push([`class`,n.langPrefix+s]):(t[e]=t[e].slice(),t[e][1]+=` `+n.langPrefix+s);let r={attrs:t};return`<pre><code${i.renderAttrs(r)}>${l}</code></pre>\n`}return`<pre><code${i.renderAttrs(a)}>${l}</code></pre>\n`},L.image=function(e,t,n,r,i){let a=e[t];return a.attrs[a.attrIndex(`alt`)][1]=i.renderInlineAsText(a.children,n,r),i.renderToken(e,t,n)},L.hardbreak=function(e,t,n){return n.xhtmlOut?`<br />
`:`<br>
`},L.softbreak=function(e,t,n){return n.breaks?n.xhtmlOut?`<br />
`:`<br>
`:`
`},L.text=function(e,t){return A(e[t].content)},L.html_block=function(e,t){return e[t].content},L.html_inline=function(e,t){return e[t].content};function R(){this.rules=Be({},L)}R.prototype.renderAttrs=function(e){let t,n,r;if(!e.attrs)return``;for(r=``,t=0,n=e.attrs.length;t<n;t++)r+=` `+A(e.attrs[t][0])+`="`+A(e.attrs[t][1])+`"`;return r},R.prototype.renderToken=function(e,t,n){let r=e[t],i=``;if(r.hidden)return``;r.block&&r.nesting!==-1&&t&&e[t-1].hidden&&(i+=`
`),i+=(r.nesting===-1?`</`:`<`)+r.tag,i+=this.renderAttrs(r),r.nesting===0&&n.xhtmlOut&&(i+=` /`);let a=!1;if(r.block&&(a=!0,r.nesting===1&&t+1<e.length)){let n=e[t+1];(n.type===`inline`||n.hidden||n.nesting===-1&&n.tag===r.tag)&&(a=!1)}return i+=a?`>
`:`>`,i},R.prototype.renderInline=function(e,t,n){let r=``,i=this.rules;for(let a=0,o=e.length;a<o;a++){let o=e[a].type;i[o]===void 0?r+=this.renderToken(e,a,t):r+=i[o](e,a,t,n,this)}return r},R.prototype.renderInlineAsText=function(e,t,n){let r=``;for(let i=0,a=e.length;i<a;i++)switch(e[i].type){case`text`:r+=e[i].content;break;case`image`:r+=this.renderInlineAsText(e[i].children,t,n);break;case`html_inline`:case`html_block`:r+=e[i].content;break;case`softbreak`:case`hardbreak`:r+=`
`;break;default:}return r},R.prototype.render=function(e,t,n){let r=``,i=this.rules;for(let a=0,o=e.length;a<o;a++){let o=e[a].type;o===`inline`?r+=this.renderInline(e[a].children,t,n):i[o]===void 0?r+=this.renderToken(e,a,t,n):r+=i[o](e,a,t,n,this)}return r};function z(){this.__rules__=[],this.__cache__=null}z.prototype.__find__=function(e){for(let t=0;t<this.__rules__.length;t++)if(this.__rules__[t].name===e)return t;return-1},z.prototype.__compile__=function(){let e=this,t=[``];e.__rules__.forEach(function(e){e.enabled&&e.alt.forEach(function(e){t.indexOf(e)<0&&t.push(e)})}),e.__cache__={},t.forEach(function(t){e.__cache__[t]=[],e.__rules__.forEach(function(n){n.enabled&&(t&&n.alt.indexOf(t)<0||e.__cache__[t].push(n.fn))})})},z.prototype.at=function(e,t,n){let r=this.__find__(e),i=n||{};if(r===-1)throw Error(`Parser rule not found: `+e);this.__rules__[r].fn=t,this.__rules__[r].alt=i.alt||[],this.__cache__=null},z.prototype.before=function(e,t,n,r){let i=this.__find__(e),a=r||{};if(i===-1)throw Error(`Parser rule not found: `+e);this.__rules__.splice(i,0,{name:t,enabled:!0,fn:n,alt:a.alt||[]}),this.__cache__=null},z.prototype.after=function(e,t,n,r){let i=this.__find__(e),a=r||{};if(i===-1)throw Error(`Parser rule not found: `+e);this.__rules__.splice(i+1,0,{name:t,enabled:!0,fn:n,alt:a.alt||[]}),this.__cache__=null},z.prototype.push=function(e,t,n){let r=n||{};this.__rules__.push({name:e,enabled:!0,fn:t,alt:r.alt||[]}),this.__cache__=null},z.prototype.enable=function(e,t){Array.isArray(e)||(e=[e]);let n=[];return e.forEach(function(e){let r=this.__find__(e);if(r<0){if(t)return;throw Error(`Rules manager: invalid rule name `+e)}this.__rules__[r].enabled=!0,n.push(e)},this),this.__cache__=null,n},z.prototype.enableOnly=function(e,t){Array.isArray(e)||(e=[e]),this.__rules__.forEach(function(e){e.enabled=!1}),this.enable(e,t)},z.prototype.disable=function(e,t){Array.isArray(e)||(e=[e]);let n=[];return e.forEach(function(e){let r=this.__find__(e);if(r<0){if(t)return;throw Error(`Rules manager: invalid rule name `+e)}this.__rules__[r].enabled=!1,n.push(e)},this),this.__cache__=null,n},z.prototype.getRules=function(e){return this.__cache__===null&&this.__compile__(),this.__cache__[e]||[]};function B(e,t,n){this.type=e,this.tag=t,this.attrs=null,this.map=null,this.nesting=n,this.level=0,this.children=null,this.content=``,this.markup=``,this.info=``,this.meta=null,this.block=!1,this.hidden=!1}B.prototype.attrIndex=function(e){if(!this.attrs)return-1;let t=this.attrs;for(let n=0,r=t.length;n<r;n++)if(t[n][0]===e)return n;return-1},B.prototype.attrPush=function(e){this.attrs?this.attrs.push(e):this.attrs=[e]},B.prototype.attrSet=function(e,t){let n=this.attrIndex(e),r=[e,t];n<0?this.attrPush(r):this.attrs[n]=r},B.prototype.attrGet=function(e){let t=this.attrIndex(e),n=null;return t>=0&&(n=this.attrs[t][1]),n},B.prototype.attrJoin=function(e,t){let n=this.attrIndex(e);n<0?this.attrPush([e,t]):this.attrs[n][1]=this.attrs[n][1]+` `+t};function st(e,t,n){this.src=e,this.env=n,this.tokens=[],this.inlineMode=!1,this.md=t}st.prototype.Token=B;var ct=/\r\n?|\n/g,lt=/\0/g;function ut(e){let t;t=e.src.replace(ct,`
`),t=t.replace(lt,`�`),e.src=t}function dt(e){let t;e.inlineMode?(t=new e.Token(`inline`,``,0),t.content=e.src,t.map=[0,1],t.children=[],e.tokens.push(t)):e.md.block.parse(e.src,e.md,e.env,e.tokens)}function ft(e){let t=e.tokens;for(let n=0,r=t.length;n<r;n++){let r=t[n];r.type===`inline`&&e.md.inline.parse(r.content,e.md,e.env,r.children)}}function pt(e){return/^<a[>\s]/i.test(e)}function mt(e){return/^<\/a\s*>/i.test(e)}function ht(e){let t=e.tokens;if(e.md.options.linkify)for(let n=0,r=t.length;n<r;n++){if(t[n].type!==`inline`||!e.md.linkify.pretest(t[n].content))continue;let r=t[n].children,i=0;for(let a=r.length-1;a>=0;a--){let o=r[a];if(o.type===`link_close`){for(a--;r[a].level!==o.level&&r[a].type!==`link_open`;)a--;continue}if(o.type===`html_inline`&&(pt(o.content)&&i>0&&i--,mt(o.content)&&i++),!(i>0)&&o.type===`text`&&e.md.linkify.test(o.content)){let i=o.content,s=e.md.linkify.match(i),c=[],l=o.level,u=0;s.length>0&&s[0].index===0&&a>0&&r[a-1].type===`text_special`&&(s=s.slice(1));for(let t=0;t<s.length;t++){let n=s[t].url,r=e.md.normalizeLink(n);if(!e.md.validateLink(r))continue;let a=s[t].text;a=s[t].schema?s[t].schema===`mailto:`&&!/^mailto:/i.test(a)?e.md.normalizeLinkText(`mailto:`+a).replace(/^mailto:/,``):e.md.normalizeLinkText(a):e.md.normalizeLinkText(`http://`+a).replace(/^http:\/\//,``);let o=s[t].index;if(o>u){let t=new e.Token(`text`,``,0);t.content=i.slice(u,o),t.level=l,c.push(t)}let d=new e.Token(`link_open`,`a`,1);d.attrs=[[`href`,r]],d.level=l++,d.markup=`linkify`,d.info=`auto`,c.push(d);let f=new e.Token(`text`,``,0);f.content=a,f.level=l,c.push(f);let p=new e.Token(`link_close`,`a`,-1);p.level=--l,p.markup=`linkify`,p.info=`auto`,c.push(p),u=s[t].lastIndex}if(u<i.length){let t=new e.Token(`text`,``,0);t.content=i.slice(u),t.level=l,c.push(t)}t[n].children=r=Ve(r,a,c)}}}}var gt=/\+-|\.\.|\?\?\?\?|!!!!|,,|--/,_t=/\((c|tm|r)\)/i,vt=/\((c|tm|r)\)/gi,yt={c:`©`,r:`®`,tm:`™`};function bt(e,t){return yt[t.toLowerCase()]}function xt(e){let t=0;for(let n=e.length-1;n>=0;n--){let r=e[n];r.type===`text`&&!t&&(r.content=r.content.replace(vt,bt)),r.type===`link_open`&&r.info===`auto`&&t--,r.type===`link_close`&&r.info===`auto`&&t++}}function St(e){let t=0;for(let n=e.length-1;n>=0;n--){let r=e[n];r.type===`text`&&!t&&gt.test(r.content)&&(r.content=r.content.replace(/\+-/g,`±`).replace(/\.{2,}/g,`…`).replace(/([?!])…/g,`$1..`).replace(/([?!]){4,}/g,`$1$1$1`).replace(/,{2,}/g,`,`).replace(/(^|[^-])---(?=[^-]|$)/gm,`$1—`).replace(/(^|\s)--(?=\s|$)/gm,`$1–`).replace(/(^|[^-\s])--(?=[^-\s]|$)/gm,`$1–`)),r.type===`link_open`&&r.info===`auto`&&t--,r.type===`link_close`&&r.info===`auto`&&t++}}function Ct(e){let t;if(e.md.options.typographer)for(t=e.tokens.length-1;t>=0;t--)e.tokens[t].type===`inline`&&(_t.test(e.tokens[t].content)&&xt(e.tokens[t].children),gt.test(e.tokens[t].content)&&St(e.tokens[t].children))}var wt=/['"]/,Tt=/['"]/g,Et=`’`;function V(e,t,n,r){e[t]||(e[t]=[]),e[t].push({pos:n,ch:r})}function Dt(e,t){let n=``,r=0;t.sort((e,t)=>e.pos-t.pos);for(let i=0;i<t.length;i++){let a=t[i];n+=e.slice(r,a.pos)+a.ch,r=a.pos+1}return n+e.slice(r)}function Ot(e,t){let n,r=[],i={};for(let a=0;a<e.length;a++){let o=e[a],s=e[a].level;for(n=r.length-1;n>=0&&!(r[n].level<=s);n--);if(r.length=n+1,o.type!==`text`)continue;let c=o.content,l=0,u=c.length;OUTER:for(;l<u;){Tt.lastIndex=l;let o=Tt.exec(c);if(!o)break;let d=!0,f=!0;l=o.index+1;let p=o[0]===`'`,m=32;if(o.index-1>=0)m=c.charCodeAt(o.index-1);else for(n=a-1;n>=0&&!(e[n].type===`softbreak`||e[n].type===`hardbreak`);n--)if(e[n].content){m=e[n].content.charCodeAt(e[n].content.length-1);break}let h=32;if(l<u)h=c.charCodeAt(l);else for(n=a+1;n<e.length&&!(e[n].type===`softbreak`||e[n].type===`hardbreak`);n++)if(e[n].content){h=e[n].content.charCodeAt(0);break}let g=P(m)||N(m),_=P(h)||N(h),v=M(m),y=M(h);if(y?d=!1:_&&(v||g||(d=!1)),v?f=!1:g&&(y||_||(f=!1)),h===34&&o[0]===`"`&&m>=48&&m<=57&&(f=d=!1),d&&f&&(d=g,f=_),!d&&!f){p&&V(i,a,o.index,Et);continue}if(f)for(n=r.length-1;n>=0;n--){let e=r[n];if(r[n].level<s)break;if(e.single===p&&r[n].level===s){e=r[n];let s,c;p?(s=t.md.options.quotes[2],c=t.md.options.quotes[3]):(s=t.md.options.quotes[0],c=t.md.options.quotes[1]),V(i,a,o.index,c),V(i,e.token,e.pos,s),r.length=n;continue OUTER}}d?r.push({token:a,pos:o.index,single:p,level:s}):f&&p&&V(i,a,o.index,Et)}}Object.keys(i).forEach(function(t){e[t].content=Dt(e[t].content,i[t])})}function kt(e){if(e.md.options.typographer)for(let t=e.tokens.length-1;t>=0;t--)e.tokens[t].type!==`inline`||!wt.test(e.tokens[t].content)||Ot(e.tokens[t].children,e)}function At(e){let t,n,r=e.tokens,i=r.length;for(let e=0;e<i;e++){if(r[e].type!==`inline`)continue;let i=r[e].children,a=i.length;for(t=0;t<a;t++)i[t].type===`text_special`&&(i[t].type=`text`);for(t=n=0;t<a;t++)i[t].type===`text`&&t+1<a&&i[t+1].type===`text`?i[t+1].content=i[t].content+i[t+1].content:(t!==n&&(i[n]=i[t]),n++);t!==n&&(i.length=n)}}var jt=[[`normalize`,ut],[`block`,dt],[`inline`,ft],[`linkify`,ht],[`replacements`,Ct],[`smartquotes`,kt],[`text_join`,At]];function Mt(){this.ruler=new z;for(let e=0;e<jt.length;e++)this.ruler.push(jt[e][0],jt[e][1])}Mt.prototype.process=function(e){let t=this.ruler.getRules(``);for(let n=0,r=t.length;n<r;n++)t[n](e)},Mt.prototype.State=st;function H(e,t,n,r){this.src=e,this.md=t,this.env=n,this.tokens=r,this.bMarks=[],this.eMarks=[],this.tShift=[],this.sCount=[],this.bsCount=[],this.blkIndent=0,this.line=0,this.lineMax=0,this.tight=!1,this.ddIndent=-1,this.listIndent=-1,this.parentType=`root`,this.level=0;let i=this.src;for(let e=0,t=0,n=0,r=0,a=i.length,o=!1;t<a;t++){let s=i.charCodeAt(t);if(!o)if(j(s)){n++,s===9?r+=4-r%4:r++;continue}else o=!0;(s===10||t===a-1)&&(s!==10&&t++,this.bMarks.push(e),this.eMarks.push(t),this.tShift.push(n),this.sCount.push(r),this.bsCount.push(0),o=!1,n=0,r=0,e=t+1)}this.bMarks.push(i.length),this.eMarks.push(i.length),this.tShift.push(0),this.sCount.push(0),this.bsCount.push(0),this.lineMax=this.bMarks.length-1}H.prototype.push=function(e,t,n){let r=new B(e,t,n);return r.block=!0,n<0&&this.level--,r.level=this.level,n>0&&this.level++,this.tokens.push(r),r},H.prototype.isEmpty=function(e){return this.bMarks[e]+this.tShift[e]>=this.eMarks[e]},H.prototype.skipEmptyLines=function(e){for(let t=this.lineMax;e<t&&!(this.bMarks[e]+this.tShift[e]<this.eMarks[e]);e++);return e},H.prototype.skipSpaces=function(e){for(let t=this.src.length;e<t&&j(this.src.charCodeAt(e));e++);return e},H.prototype.skipSpacesBack=function(e,t){if(e<=t)return e;for(;e>t;)if(!j(this.src.charCodeAt(--e)))return e+1;return e},H.prototype.skipChars=function(e,t){for(let n=this.src.length;e<n&&this.src.charCodeAt(e)===t;e++);return e},H.prototype.skipCharsBack=function(e,t,n){if(e<=n)return e;for(;e>n;)if(t!==this.src.charCodeAt(--e))return e+1;return e},H.prototype.getLines=function(e,t,n,r){if(e>=t)return``;let i=Array(t-e);for(let a=0,o=e;o<t;o++,a++){let e=0,s=this.bMarks[o],c=s,l;for(l=o+1<t||r?this.eMarks[o]+1:this.eMarks[o];c<l&&e<n;){let t=this.src.charCodeAt(c);if(j(t))t===9?e+=4-(e+this.bsCount[o])%4:e++;else if(c-s<this.tShift[o])e++;else break;c++}e>n?i[a]=Array(e-n+1).join(` `)+this.src.slice(c,l):i[a]=this.src.slice(c,l)}return i.join(``)},H.prototype.Token=B;var Nt=65536;function Pt(e,t){let n=e.bMarks[t]+e.tShift[t],r=e.eMarks[t];return e.src.slice(n,r)}function Ft(e){let t=[],n=e.length,r=0,i=e.charCodeAt(r),a=!1,o=0,s=``;for(;r<n;)i===124&&(a?(s+=e.substring(o,r-1),o=r):(t.push(s+e.substring(o,r)),s=``,o=r+1)),a=i===92,r++,i=e.charCodeAt(r);return t.push(s+e.substring(o)),t}function It(e,t,n,r){if(t+2>n)return!1;let i=t+1;if(e.sCount[i]<e.blkIndent||e.sCount[i]-e.blkIndent>=4)return!1;let a=e.bMarks[i]+e.tShift[i];if(a>=e.eMarks[i])return!1;let o=e.src.charCodeAt(a++);if(o!==124&&o!==45&&o!==58||a>=e.eMarks[i])return!1;let s=e.src.charCodeAt(a++);if(s!==124&&s!==45&&s!==58&&!j(s)||o===45&&j(s))return!1;for(;a<e.eMarks[i];){let t=e.src.charCodeAt(a);if(t!==124&&t!==45&&t!==58&&!j(t))return!1;a++}let c=Pt(e,t+1),l=c.split(`|`),u=[];for(let e=0;e<l.length;e++){let t=l[e].trim();if(!t){if(e===0||e===l.length-1)continue;return!1}if(!/^:?-+:?$/.test(t))return!1;t.charCodeAt(t.length-1)===58?u.push(t.charCodeAt(0)===58?`center`:`right`):t.charCodeAt(0)===58?u.push(`left`):u.push(``)}if(c=Pt(e,t).trim(),c.indexOf(`|`)===-1||e.sCount[t]-e.blkIndent>=4)return!1;l=Ft(c),l.length&&l[0]===``&&l.shift(),l.length&&l[l.length-1]===``&&l.pop();let d=l.length;if(d===0||d!==u.length)return!1;if(r)return!0;let f=e.parentType;e.parentType=`table`;let p=e.md.block.ruler.getRules(`blockquote`),m=e.push(`table_open`,`table`,1),h=[t,0];m.map=h;let g=e.push(`thead_open`,`thead`,1);g.map=[t,t+1];let _=e.push(`tr_open`,`tr`,1);_.map=[t,t+1];for(let t=0;t<l.length;t++){let n=e.push(`th_open`,`th`,1);u[t]&&(n.attrs=[[`style`,`text-align:`+u[t]]]);let r=e.push(`inline`,``,0);r.content=l[t].trim(),r.children=[],e.push(`th_close`,`th`,-1)}e.push(`tr_close`,`tr`,-1),e.push(`thead_close`,`thead`,-1);let v,y=0;for(i=t+2;i<n&&!(e.sCount[i]<e.blkIndent);i++){let r=!1;for(let t=0,a=p.length;t<a;t++)if(p[t](e,i,n,!0)){r=!0;break}if(r||(c=Pt(e,i).trim(),!c)||e.sCount[i]-e.blkIndent>=4||(l=Ft(c),l.length&&l[0]===``&&l.shift(),l.length&&l[l.length-1]===``&&l.pop(),y+=d-l.length,y>Nt))break;if(i===t+2){let n=e.push(`tbody_open`,`tbody`,1);n.map=v=[t+2,0]}let a=e.push(`tr_open`,`tr`,1);a.map=[i,i+1];for(let t=0;t<d;t++){let n=e.push(`td_open`,`td`,1);u[t]&&(n.attrs=[[`style`,`text-align:`+u[t]]]);let r=e.push(`inline`,``,0);r.content=l[t]?l[t].trim():``,r.children=[],e.push(`td_close`,`td`,-1)}e.push(`tr_close`,`tr`,-1)}return v&&(e.push(`tbody_close`,`tbody`,-1),v[1]=i),e.push(`table_close`,`table`,-1),h[1]=i,e.parentType=f,e.line=i,!0}function Lt(e,t,n){if(e.sCount[t]-e.blkIndent<4)return!1;let r=t+1,i=r;for(;r<n;){if(e.isEmpty(r)){r++;continue}if(e.sCount[r]-e.blkIndent>=4){r++,i=r;continue}break}e.line=i;let a=e.push(`code_block`,`code`,0);return a.content=e.getLines(t,i,4+e.blkIndent,!1)+`
`,a.map=[t,e.line],!0}function Rt(e,t,n,r){let i=e.bMarks[t]+e.tShift[t],a=e.eMarks[t];if(e.sCount[t]-e.blkIndent>=4||i+3>a)return!1;let o=e.src.charCodeAt(i);if(o!==126&&o!==96)return!1;let s=i;i=e.skipChars(i,o);let c=i-s;if(c<3)return!1;let l=e.src.slice(s,i),u=e.src.slice(i,a);if(o===96&&u.indexOf(String.fromCharCode(o))>=0)return!1;if(r)return!0;let d=t,f=!1;for(;d++,!(d>=n||(i=s=e.bMarks[d]+e.tShift[d],a=e.eMarks[d],i<a&&e.sCount[d]<e.blkIndent));)if(e.src.charCodeAt(i)===o&&!(e.sCount[d]-e.blkIndent>=4)&&(i=e.skipChars(i,o),!(i-s<c)&&(i=e.skipSpaces(i),!(i<a)))){f=!0;break}c=e.sCount[t],e.line=d+ +!!f;let p=e.push(`fence`,`code`,0);return p.info=u,p.content=e.getLines(t+1,d,c,!0),p.markup=l,p.map=[t,e.line],!0}function zt(e,t,n,r){let i=e.bMarks[t]+e.tShift[t],a=e.eMarks[t],o=e.lineMax;if(e.sCount[t]-e.blkIndent>=4||e.src.charCodeAt(i)!==62)return!1;if(r)return!0;let s=[],c=[],l=[],u=[],d=e.md.block.ruler.getRules(`blockquote`),f=e.parentType;e.parentType=`blockquote`;let p=!1,m;for(m=t;m<n;m++){let t=e.sCount[m]<e.blkIndent;if(i=e.bMarks[m]+e.tShift[m],a=e.eMarks[m],i>=a)break;if(e.src.charCodeAt(i++)===62&&!t){let t=e.sCount[m]+1,n,r;e.src.charCodeAt(i)===32?(i++,t++,r=!1,n=!0):e.src.charCodeAt(i)===9?(n=!0,(e.bsCount[m]+t)%4==3?(i++,t++,r=!1):r=!0):n=!1;let o=t;for(s.push(e.bMarks[m]),e.bMarks[m]=i;i<a;){let t=e.src.charCodeAt(i);if(j(t))t===9?o+=4-(o+e.bsCount[m]+ +!!r)%4:o++;else break;i++}p=i>=a,c.push(e.bsCount[m]),e.bsCount[m]=e.sCount[m]+1+ +!!n,l.push(e.sCount[m]),e.sCount[m]=o-t,u.push(e.tShift[m]),e.tShift[m]=i-e.bMarks[m];continue}if(p)break;let r=!1;for(let t=0,i=d.length;t<i;t++)if(d[t](e,m,n,!0)){r=!0;break}if(r){e.lineMax=m,e.blkIndent!==0&&(s.push(e.bMarks[m]),c.push(e.bsCount[m]),u.push(e.tShift[m]),l.push(e.sCount[m]),e.sCount[m]-=e.blkIndent);break}s.push(e.bMarks[m]),c.push(e.bsCount[m]),u.push(e.tShift[m]),l.push(e.sCount[m]),e.sCount[m]=-1}let h=e.blkIndent;e.blkIndent=0;let g=e.push(`blockquote_open`,`blockquote`,1);g.markup=`>`;let _=[t,0];g.map=_,e.md.block.tokenize(e,t,m);let v=e.push(`blockquote_close`,`blockquote`,-1);v.markup=`>`,e.lineMax=o,e.parentType=f,_[1]=e.line;for(let n=0;n<u.length;n++)e.bMarks[n+t]=s[n],e.tShift[n+t]=u[n],e.sCount[n+t]=l[n],e.bsCount[n+t]=c[n];return e.blkIndent=h,!0}function Bt(e,t,n,r){let i=e.eMarks[t];if(e.sCount[t]-e.blkIndent>=4)return!1;let a=e.bMarks[t]+e.tShift[t],o=e.src.charCodeAt(a++);if(o!==42&&o!==45&&o!==95)return!1;let s=1;for(;a<i;){let t=e.src.charCodeAt(a++);if(t!==o&&!j(t))return!1;t===o&&s++}if(s<3)return!1;if(r)return!0;e.line=t+1;let c=e.push(`hr`,`hr`,0);return c.map=[t,e.line],c.markup=Array(s+1).join(String.fromCharCode(o)),!0}function Vt(e,t){let n=e.eMarks[t],r=e.bMarks[t]+e.tShift[t],i=e.src.charCodeAt(r++);return i!==42&&i!==45&&i!==43||r<n&&!j(e.src.charCodeAt(r))?-1:r}function Ht(e,t){let n=e.bMarks[t]+e.tShift[t],r=e.eMarks[t],i=n;if(i+1>=r)return-1;let a=e.src.charCodeAt(i++);if(a<48||a>57)return-1;for(;;){if(i>=r)return-1;if(a=e.src.charCodeAt(i++),a>=48&&a<=57){if(i-n>=10)return-1;continue}if(a===41||a===46)break;return-1}return i<r&&(a=e.src.charCodeAt(i),!j(a))?-1:i}function Ut(e,t){let n=e.level+2;for(let r=t+2,i=e.tokens.length-2;r<i;r++)e.tokens[r].level===n&&e.tokens[r].type===`paragraph_open`&&(e.tokens[r+2].hidden=!0,e.tokens[r].hidden=!0,r+=2)}function Wt(e,t,n,r){let i,a,o,s,c=t,l=!0;if(e.sCount[c]-e.blkIndent>=4||e.listIndent>=0&&e.sCount[c]-e.listIndent>=4&&e.sCount[c]<e.blkIndent)return!1;let u=!1;r&&e.parentType===`paragraph`&&e.sCount[c]>=e.blkIndent&&(u=!0);let d,f,p;if((p=Ht(e,c))>=0){if(d=!0,o=e.bMarks[c]+e.tShift[c],f=Number(e.src.slice(o,p-1)),u&&f!==1)return!1}else if((p=Vt(e,c))>=0)d=!1;else return!1;if(u&&e.skipSpaces(p)>=e.eMarks[c])return!1;if(r)return!0;let m=e.src.charCodeAt(p-1),h=e.tokens.length;d?(s=e.push(`ordered_list_open`,`ol`,1),f!==1&&(s.attrs=[[`start`,f]])):s=e.push(`bullet_list_open`,`ul`,1);let g=[c,0];s.map=g,s.markup=String.fromCharCode(m);let _=!1,v=e.md.block.ruler.getRules(`list`),y=e.parentType;for(e.parentType=`list`;c<n;){a=p,i=e.eMarks[c];let t=e.sCount[c]+p-(e.bMarks[c]+e.tShift[c]),r=t;for(;a<i;){let t=e.src.charCodeAt(a);if(t===9)r+=4-(r+e.bsCount[c])%4;else if(t===32)r++;else break;a++}let u=a,f;f=u>=i?1:r-t,f>4&&(f=1);let h=t+f;s=e.push(`list_item_open`,`li`,1),s.markup=String.fromCharCode(m);let g=[c,0];s.map=g,d&&(s.info=e.src.slice(o,p-1));let y=e.tight,b=e.tShift[c],x=e.sCount[c],S=e.listIndent;if(e.listIndent=e.blkIndent,e.blkIndent=h,e.tight=!0,e.tShift[c]=u-e.bMarks[c],e.sCount[c]=r,u>=i&&e.isEmpty(c+1)?e.line=Math.min(e.line+2,n):e.md.block.tokenize(e,c,n,!0),(!e.tight||_)&&(l=!1),_=e.line-c>1&&e.isEmpty(e.line-1),e.blkIndent=e.listIndent,e.listIndent=S,e.tShift[c]=b,e.sCount[c]=x,e.tight=y,s=e.push(`list_item_close`,`li`,-1),s.markup=String.fromCharCode(m),c=e.line,g[1]=c,c>=n||e.sCount[c]<e.blkIndent||e.sCount[c]-e.blkIndent>=4)break;let C=!1;for(let t=0,r=v.length;t<r;t++)if(v[t](e,c,n,!0)){C=!0;break}if(C)break;if(d){if(p=Ht(e,c),p<0)break;o=e.bMarks[c]+e.tShift[c]}else if(p=Vt(e,c),p<0)break;if(m!==e.src.charCodeAt(p-1))break}return s=d?e.push(`ordered_list_close`,`ol`,-1):e.push(`bullet_list_close`,`ul`,-1),s.markup=String.fromCharCode(m),g[1]=c,e.line=c,e.parentType=y,l&&Ut(e,h),!0}function Gt(e,t,n,r){let i=e.bMarks[t]+e.tShift[t],a=e.eMarks[t],o=t+1;if(e.sCount[t]-e.blkIndent>=4||e.src.charCodeAt(i)!==91)return!1;function s(t){let n=e.lineMax;if(t>=n||e.isEmpty(t))return null;let r=!1;if(e.sCount[t]-e.blkIndent>3&&(r=!0),e.sCount[t]<0&&(r=!0),!r){let r=e.md.block.ruler.getRules(`reference`),i=e.parentType;e.parentType=`reference`;let a=!1;for(let i=0,o=r.length;i<o;i++)if(r[i](e,t,n,!0)){a=!0;break}if(e.parentType=i,a)return null}let i=e.bMarks[t]+e.tShift[t],a=e.eMarks[t];return e.src.slice(i,a+1)}let c=e.src.slice(i,a+1);a=c.length;let l=-1;for(i=1;i<a;i++){let e=c.charCodeAt(i);if(e===91)return!1;if(e===93){l=i;break}else if(e===10){let e=s(o);e!==null&&(c+=e,a=c.length,o++)}else if(e===92&&(i++,i<a&&c.charCodeAt(i)===10)){let e=s(o);e!==null&&(c+=e,a=c.length,o++)}}if(l<0||c.charCodeAt(l+1)!==58)return!1;for(i=l+2;i<a;i++){let e=c.charCodeAt(i);if(e===10){let e=s(o);e!==null&&(c+=e,a=c.length,o++)}else if(!j(e))break}let u=e.md.helpers.parseLinkDestination(c,i,a);if(!u.ok)return!1;let d=e.md.normalizeLink(u.str);if(!e.md.validateLink(d))return!1;i=u.pos;let f=i,p=o,m=i;for(;i<a;i++){let e=c.charCodeAt(i);if(e===10){let e=s(o);e!==null&&(c+=e,a=c.length,o++)}else if(!j(e))break}let h=e.md.helpers.parseLinkTitle(c,i,a);for(;h.can_continue;){let t=s(o);if(t===null)break;c+=t,i=a,a=c.length,o++,h=e.md.helpers.parseLinkTitle(c,i,a,h)}let g;for(i<a&&m!==i&&h.ok?(g=h.str,i=h.pos):(g=``,i=f,o=p);i<a&&j(c.charCodeAt(i));)i++;if(i<a&&c.charCodeAt(i)!==10&&g)for(g=``,i=f,o=p;i<a&&j(c.charCodeAt(i));)i++;if(i<a&&c.charCodeAt(i)!==10)return!1;let _=F(c.slice(1,l));return _?r?!0:(e.env.references===void 0&&(e.env.references={}),e.env.references[_]===void 0&&(e.env.references[_]={title:g,href:d}),e.line=o,!0):!1}var Kt=`address.article.aside.base.basefont.blockquote.body.caption.center.col.colgroup.dd.details.dialog.dir.div.dl.dt.fieldset.figcaption.figure.footer.form.frame.frameset.h1.h2.h3.h4.h5.h6.head.header.hr.html.iframe.legend.li.link.main.menu.menuitem.nav.noframes.ol.optgroup.option.p.param.search.section.summary.table.tbody.td.tfoot.th.thead.title.tr.track.ul`.split(`.`),qt=RegExp(`^(?:<[A-Za-z][A-Za-z0-9\\-]*(?:\\s+[a-zA-Z_:][a-zA-Z0-9:._-]*(?:\\s*=\\s*(?:[^"'=<>\`\\x00-\\x20]+|'[^']*'|"[^"]*"))?)*\\s*\\/?>|<\\/[A-Za-z][A-Za-z0-9\\-]*\\s*>|<!---?>|<!--(?:[^-]|-[^-]|--[^>])*-->|<[?][\\s\\S]*?[?]>|<![A-Za-z][^>]*>|<!\\[CDATA\\[[\\s\\S]*?\\]\\]>)`),Jt=RegExp(`^(?:<[A-Za-z][A-Za-z0-9\\-]*(?:\\s+[a-zA-Z_:][a-zA-Z0-9:._-]*(?:\\s*=\\s*(?:[^"'=<>\`\\x00-\\x20]+|'[^']*'|"[^"]*"))?)*\\s*\\/?>|<\\/[A-Za-z][A-Za-z0-9\\-]*\\s*>)`),U=[[/^<(script|pre|style|textarea)(?=(\s|>|$))/i,/<\/(script|pre|style|textarea)>/i,!0],[/^<!--/,/-->/,!0],[/^<\?/,/\?>/,!0],[/^<![A-Z]/,/>/,!0],[/^<!\[CDATA\[/,/\]\]>/,!0],[RegExp(`^</?(`+Kt.join(`|`)+`)(?=(\\s|/?>|$))`,`i`),/^$/,!0],[RegExp(Jt.source+`\\s*$`),/^$/,!1]];function Yt(e,t,n,r){let i=e.bMarks[t]+e.tShift[t],a=e.eMarks[t];if(e.sCount[t]-e.blkIndent>=4||!e.md.options.html||e.src.charCodeAt(i)!==60)return!1;let o=e.src.slice(i,a),s=0;for(;s<U.length&&!U[s][0].test(o);s++);if(s===U.length)return!1;if(r)return U[s][2];let c=t+1,l=U[s][1].test(``);if(!U[s][1].test(o)){for(;c<n&&!(e.sCount[c]<e.blkIndent&&(l||!e.isEmpty(c)));c++)if(i=e.bMarks[c]+e.tShift[c],a=e.eMarks[c],o=e.src.slice(i,a),U[s][1].test(o)){o.length!==0&&c++;break}}e.line=c;let u=e.push(`html_block`,``,0);return u.map=[t,c],u.content=e.getLines(t,c,e.blkIndent,!0),!0}function Xt(e,t,n,r){let i=e.bMarks[t]+e.tShift[t],a=e.eMarks[t];if(e.sCount[t]-e.blkIndent>=4)return!1;let o=e.src.charCodeAt(i);if(o!==35||i>=a)return!1;let s=1;for(o=e.src.charCodeAt(++i);o===35&&i<a&&s<=6;)s++,o=e.src.charCodeAt(++i);if(s>6||i<a&&!j(o))return!1;if(r)return!0;a=e.skipSpacesBack(a,i);let c=e.skipCharsBack(a,35,i);c>i&&j(e.src.charCodeAt(c-1))&&(a=c),e.line=t+1;let l=e.push(`heading_open`,`h`+String(s),1);l.markup=`########`.slice(0,s),l.map=[t,e.line];let u=e.push(`inline`,``,0);u.content=I(e.src.slice(i,a)),u.map=[t,e.line],u.children=[];let d=e.push(`heading_close`,`h`+String(s),-1);return d.markup=`########`.slice(0,s),!0}function Zt(e,t,n){let r=e.md.block.ruler.getRules(`paragraph`);if(e.sCount[t]-e.blkIndent>=4)return!1;let i=e.parentType;e.parentType=`paragraph`;let a=0,o,s=t+1;for(;s<n&&!e.isEmpty(s);s++){if(e.sCount[s]-e.blkIndent>3)continue;if(e.sCount[s]>=e.blkIndent){let t=e.bMarks[s]+e.tShift[s],n=e.eMarks[s];if(t<n&&(o=e.src.charCodeAt(t),(o===45||o===61)&&(t=e.skipChars(t,o),t=e.skipSpaces(t),t>=n))){a=o===61?1:2;break}}if(e.sCount[s]<0)continue;let t=!1;for(let i=0,a=r.length;i<a;i++)if(r[i](e,s,n,!0)){t=!0;break}if(t)break}if(!a)return e.parentType=i,!1;let c=I(e.getLines(t,s,e.blkIndent,!1));e.line=s+1;let l=e.push(`heading_open`,`h`+String(a),1);l.markup=String.fromCharCode(o),l.map=[t,e.line];let u=e.push(`inline`,``,0);u.content=c,u.map=[t,e.line-1],u.children=[];let d=e.push(`heading_close`,`h`+String(a),-1);return d.markup=String.fromCharCode(o),e.parentType=i,!0}function Qt(e,t,n){let r=e.md.block.ruler.getRules(`paragraph`),i=e.parentType,a=t+1;for(e.parentType=`paragraph`;a<n&&!e.isEmpty(a);a++){if(e.sCount[a]-e.blkIndent>3||e.sCount[a]<0)continue;let t=!1;for(let i=0,o=r.length;i<o;i++)if(r[i](e,a,n,!0)){t=!0;break}if(t)break}let o=I(e.getLines(t,a,e.blkIndent,!1));e.line=a;let s=e.push(`paragraph_open`,`p`,1);s.map=[t,e.line];let c=e.push(`inline`,``,0);return c.content=o,c.map=[t,e.line],c.children=[],e.push(`paragraph_close`,`p`,-1),e.parentType=i,!0}var W=[[`table`,It,[`paragraph`,`reference`]],[`code`,Lt],[`fence`,Rt,[`paragraph`,`reference`,`blockquote`,`list`]],[`blockquote`,zt,[`paragraph`,`reference`,`blockquote`,`list`]],[`hr`,Bt,[`paragraph`,`reference`,`blockquote`,`list`]],[`list`,Wt,[`paragraph`,`reference`,`blockquote`]],[`reference`,Gt],[`html_block`,Yt,[`paragraph`,`reference`,`blockquote`]],[`heading`,Xt,[`paragraph`,`reference`,`blockquote`]],[`lheading`,Zt],[`paragraph`,Qt]];function G(){this.ruler=new z;for(let e=0;e<W.length;e++)this.ruler.push(W[e][0],W[e][1],{alt:(W[e][2]||[]).slice()})}G.prototype.tokenize=function(e,t,n){let r=this.ruler.getRules(``),i=r.length,a=e.md.options.maxNesting,o=t,s=!1;for(;o<n&&(e.line=o=e.skipEmptyLines(o),!(o>=n||e.sCount[o]<e.blkIndent));){if(e.level>=a){e.line=n;break}let t=e.line,c=!1;for(let a=0;a<i;a++)if(c=r[a](e,o,n,!1),c){if(t>=e.line)throw Error(`block rule didn't increment state.line`);break}if(!c)throw Error(`none of the block rules matched`);e.tight=!s,e.isEmpty(e.line-1)&&(s=!0),o=e.line,o<n&&e.isEmpty(o)&&(s=!0,o++,e.line=o)}},G.prototype.parse=function(e,t,n,r){if(!e)return;let i=new this.State(e,t,n,r);this.tokenize(i,i.line,i.lineMax)},G.prototype.State=H;function K(e,t,n,r){this.src=e,this.env=n,this.md=t,this.tokens=r,this.tokens_meta=Array(r.length),this.pos=0,this.posMax=this.src.length,this.level=0,this.pending=``,this.pendingLevel=0,this.cache={},this.delimiters=[],this._prev_delimiters=[],this.backticks={},this.backticksScanned=!1,this.linkLevel=0}K.prototype.pushPending=function(){let e=new B(`text`,``,0);return e.content=this.pending,e.level=this.pendingLevel,this.tokens.push(e),this.pending=``,e},K.prototype.push=function(e,t,n){this.pending&&this.pushPending();let r=new B(e,t,n),i=null;return n<0&&(this.level--,this.delimiters=this._prev_delimiters.pop()),r.level=this.level,n>0&&(this.level++,this._prev_delimiters.push(this.delimiters),this.delimiters=[],i={delimiters:this.delimiters}),this.pendingLevel=this.level,this.tokens.push(r),this.tokens_meta.push(i),r},K.prototype.scanDelims=function(e,t){let n=this.posMax,r=this.src.charCodeAt(e),i;if(e===0)i=32;else if(e===1)i=this.src.charCodeAt(0),(i&63488)==55296&&(i=65533);else if(i=this.src.charCodeAt(e-1),(i&64512)==56320){let t=this.src.charCodeAt(e-2);i=(t&64512)==55296?65536+(t-55296<<10)+(i-56320):65533}else(i&64512)==55296&&(i=65533);let a=e;for(;a<n&&this.src.charCodeAt(a)===r;)a++;let o=a-e,s=a<n?this.src.charCodeAt(a):32;if((s&64512)==55296){let e=this.src.charCodeAt(a+1);s=(e&64512)==56320?65536+(s-55296<<10)+(e-56320):65533}else(s&64512)==56320&&(s=65533);let c=P(i)||N(i),l=P(s)||N(s),u=M(i),d=M(s),f=!d&&(!l||u||c),p=!u&&(!c||d||l);return{can_open:f&&(t||!p||c),can_close:p&&(t||!f||l),length:o}},K.prototype.Token=B;function $t(e){switch(e){case 10:case 33:case 35:case 36:case 37:case 38:case 42:case 43:case 45:case 58:case 60:case 61:case 62:case 64:case 91:case 92:case 93:case 94:case 95:case 96:case 123:case 125:case 126:return!0;default:return!1}}function en(e,t){let n=e.pos;for(;n<e.posMax&&!$t(e.src.charCodeAt(n));)n++;return n===e.pos?!1:(t||(e.pending+=e.src.slice(e.pos,n)),e.pos=n,!0)}var tn=/(?:^|[^a-z0-9.+-])([a-z][a-z0-9.+-]*)$/i;function nn(e,t){if(!e.md.options.linkify||e.linkLevel>0)return!1;let n=e.pos,r=e.posMax;if(n+3>r||e.src.charCodeAt(n)!==58||e.src.charCodeAt(n+1)!==47||e.src.charCodeAt(n+2)!==47)return!1;let i=e.pending.match(tn);if(!i)return!1;let a=i[1],o=e.md.linkify.matchAtStart(e.src.slice(n-a.length));if(!o)return!1;let s=o.url;if(s.length<=a.length)return!1;let c=s.length;for(;c>0&&s.charCodeAt(c-1)===42;)c--;c!==s.length&&(s=s.slice(0,c));let l=e.md.normalizeLink(s);if(!e.md.validateLink(l))return!1;if(!t){e.pending=e.pending.slice(0,-a.length);let t=e.push(`link_open`,`a`,1);t.attrs=[[`href`,l]],t.markup=`linkify`,t.info=`auto`;let n=e.push(`text`,``,0);n.content=e.md.normalizeLinkText(s);let r=e.push(`link_close`,`a`,-1);r.markup=`linkify`,r.info=`auto`}return e.pos+=s.length-a.length,!0}function rn(e,t){let n=e.pos;if(e.src.charCodeAt(n)!==10)return!1;let r=e.pending.length-1,i=e.posMax;if(!t)if(r>=0&&e.pending.charCodeAt(r)===32)if(r>=1&&e.pending.charCodeAt(r-1)===32){let t=r-1;for(;t>=1&&e.pending.charCodeAt(t-1)===32;)t--;e.pending=e.pending.slice(0,t),e.push(`hardbreak`,`br`,0)}else e.pending=e.pending.slice(0,-1),e.push(`softbreak`,`br`,0);else e.push(`softbreak`,`br`,0);for(n++;n<i&&j(e.src.charCodeAt(n));)n++;return e.pos=n,!0}var an=[];for(let e=0;e<256;e++)an.push(0);`\\!"#$%&'()*+,./:;<=>?@[]^_\`{|}~-`.split(``).forEach(function(e){an[e.charCodeAt(0)]=1});function on(e,t){let n=e.pos,r=e.posMax;if(e.src.charCodeAt(n)!==92||(n++,n>=r))return!1;let i=e.src.charCodeAt(n);if(i===10){for(t||e.push(`hardbreak`,`br`,0),n++;n<r&&(i=e.src.charCodeAt(n),j(i));)n++;return e.pos=n,!0}if(i===32){if(!t){let t=e.push(`text_special`,``,0);t.content=`\\`,t.markup=`\\`,t.info=`escape`}return e.pos=n,!0}let a=e.src[n];if(i>=55296&&i<=56319&&n+1<r){let t=e.src.charCodeAt(n+1);t>=56320&&t<=57343&&(a+=e.src[n+1],n++)}let o=`\\`+a;if(!t){let t=e.push(`text_special`,``,0);i<256&&an[i]!==0?t.content=a:t.content=o,t.markup=o,t.info=`escape`}return e.pos=n+1,!0}function sn(e,t){let n=e.pos;if(e.src.charCodeAt(n)!==96)return!1;let r=n;n++;let i=e.posMax;for(;n<i&&e.src.charCodeAt(n)===96;)n++;let a=e.src.slice(r,n),o=a.length;if(e.backticksScanned&&(e.backticks[o]||0)<=r)return t||(e.pending+=a),e.pos+=o,!0;let s=n,c;for(;(c=e.src.indexOf("`",s))!==-1;){for(s=c+1;s<i&&e.src.charCodeAt(s)===96;)s++;let r=s-c;if(r===o){if(!t){let t=e.push(`code_inline`,`code`,0);t.markup=a,t.content=e.src.slice(n,c).replace(/\n/g,` `).replace(/^ (.+) $/,`$1`)}return e.pos=s,!0}e.backticks[r]=c}return e.backticksScanned=!0,t||(e.pending+=a),e.pos+=o,!0}function cn(e,t){let n=e.pos,r=e.src.charCodeAt(n);if(t||r!==126)return!1;let i=e.scanDelims(e.pos,!0),a=i.length,o=String.fromCharCode(r);if(a<2)return!1;let s;a%2&&(s=e.push(`text`,``,0),s.content=o,a--);for(let t=0;t<a;t+=2)s=e.push(`text`,``,0),s.content=o+o,e.delimiters.push({marker:r,length:0,token:e.tokens.length-1,end:-1,open:i.can_open,close:i.can_close});return e.pos+=i.length,!0}function ln(e,t){let n,r=[],i=t.length;for(let a=0;a<i;a++){let i=t[a];if(i.marker!==126||i.end===-1)continue;let o=t[i.end];n=e.tokens[i.token],n.type=`s_open`,n.tag=`s`,n.nesting=1,n.markup=`~~`,n.content=``,n=e.tokens[o.token],n.type=`s_close`,n.tag=`s`,n.nesting=-1,n.markup=`~~`,n.content=``,e.tokens[o.token-1].type===`text`&&e.tokens[o.token-1].content===`~`&&r.push(o.token-1)}for(;r.length;){let t=r.pop(),i=t+1;for(;i<e.tokens.length&&e.tokens[i].type===`s_close`;)i++;i--,t!==i&&(n=e.tokens[i],e.tokens[i]=e.tokens[t],e.tokens[t]=n)}}function un(e){let t=e.tokens_meta,n=e.tokens_meta.length;ln(e,e.delimiters);for(let r=0;r<n;r++)t[r]&&t[r].delimiters&&ln(e,t[r].delimiters)}var dn={tokenize:cn,postProcess:un};function fn(e,t){let n=e.pos,r=e.src.charCodeAt(n);if(t||r!==95&&r!==42)return!1;let i=e.scanDelims(e.pos,r===42);for(let t=0;t<i.length;t++){let t=e.push(`text`,``,0);t.content=String.fromCharCode(r),e.delimiters.push({marker:r,length:i.length,token:e.tokens.length-1,end:-1,open:i.can_open,close:i.can_close})}return e.pos+=i.length,!0}function pn(e,t){let n=t.length;for(let r=n-1;r>=0;r--){let n=t[r];if(n.marker!==95&&n.marker!==42||n.end===-1)continue;let i=t[n.end],a=r>0&&t[r-1].end===n.end+1&&t[r-1].marker===n.marker&&t[r-1].token===n.token-1&&t[n.end+1].token===i.token+1,o=String.fromCharCode(n.marker),s=e.tokens[n.token];s.type=a?`strong_open`:`em_open`,s.tag=a?`strong`:`em`,s.nesting=1,s.markup=a?o+o:o,s.content=``;let c=e.tokens[i.token];c.type=a?`strong_close`:`em_close`,c.tag=a?`strong`:`em`,c.nesting=-1,c.markup=a?o+o:o,c.content=``,a&&(e.tokens[t[r-1].token].content=``,e.tokens[t[n.end+1].token].content=``,r--)}}function mn(e){let t=e.tokens_meta,n=e.tokens_meta.length;pn(e,e.delimiters);for(let r=0;r<n;r++)t[r]&&t[r].delimiters&&pn(e,t[r].delimiters)}var hn={tokenize:fn,postProcess:mn};function gn(e,t){let n,r,i,a,o=``,s=``,c=e.pos,l=!0;if(e.src.charCodeAt(e.pos)!==91)return!1;let u=e.pos,d=e.posMax,f=e.pos+1,p=e.md.helpers.parseLinkLabel(e,e.pos,!0);if(p<0)return!1;let m=p+1;if(m<d&&e.src.charCodeAt(m)===40){for(l=!1,m++;m<d&&(n=e.src.charCodeAt(m),!(!j(n)&&n!==10));m++);if(m>=d)return!1;if(c=m,i=e.md.helpers.parseLinkDestination(e.src,m,e.posMax),i.ok){for(o=e.md.normalizeLink(i.str),e.md.validateLink(o)?m=i.pos:o=``,c=m;m<d&&(n=e.src.charCodeAt(m),!(!j(n)&&n!==10));m++);if(i=e.md.helpers.parseLinkTitle(e.src,m,e.posMax),m<d&&c!==m&&i.ok)for(s=i.str,m=i.pos;m<d&&(n=e.src.charCodeAt(m),!(!j(n)&&n!==10));m++);}(m>=d||e.src.charCodeAt(m)!==41)&&(l=!0),m++}if(l){if(e.env.references===void 0)return!1;if(m<d&&e.src.charCodeAt(m)===91?(c=m+1,m=e.md.helpers.parseLinkLabel(e,m),m>=0?r=e.src.slice(c,m++):m=p+1):m=p+1,r||=e.src.slice(f,p),a=e.env.references[F(r)],!a)return e.pos=u,!1;o=a.href,s=a.title}if(!t){e.pos=f,e.posMax=p;let t=e.push(`link_open`,`a`,1),n=[[`href`,o]];t.attrs=n,s&&n.push([`title`,s]),e.linkLevel++,e.md.inline.tokenize(e),e.linkLevel--,e.push(`link_close`,`a`,-1)}return e.pos=m,e.posMax=d,!0}function _n(e,t){let n,r,i,a,o,s,c,l,u=``,d=e.pos,f=e.posMax;if(e.src.charCodeAt(e.pos)!==33||e.src.charCodeAt(e.pos+1)!==91)return!1;let p=e.pos+2,m=e.md.helpers.parseLinkLabel(e,e.pos+1,!1);if(m<0)return!1;if(a=m+1,a<f&&e.src.charCodeAt(a)===40){for(a++;a<f&&(n=e.src.charCodeAt(a),!(!j(n)&&n!==10));a++);if(a>=f)return!1;for(l=a,s=e.md.helpers.parseLinkDestination(e.src,a,e.posMax),s.ok&&(u=e.md.normalizeLink(s.str),e.md.validateLink(u)?a=s.pos:u=``),l=a;a<f&&(n=e.src.charCodeAt(a),!(!j(n)&&n!==10));a++);if(s=e.md.helpers.parseLinkTitle(e.src,a,e.posMax),a<f&&l!==a&&s.ok)for(c=s.str,a=s.pos;a<f&&(n=e.src.charCodeAt(a),!(!j(n)&&n!==10));a++);else c=``;if(a>=f||e.src.charCodeAt(a)!==41)return e.pos=d,!1;a++}else{if(e.env.references===void 0)return!1;if(a<f&&e.src.charCodeAt(a)===91?(l=a+1,a=e.md.helpers.parseLinkLabel(e,a),a>=0?i=e.src.slice(l,a++):a=m+1):a=m+1,i||=e.src.slice(p,m),o=e.env.references[F(i)],!o)return e.pos=d,!1;u=o.href,c=o.title}if(!t){r=e.src.slice(p,m);let t=[];e.md.inline.parse(r,e.md,e.env,t);let n=e.push(`image`,`img`,0),i=[[`src`,u],[`alt`,``]];n.attrs=i,n.children=t,n.content=r,c&&i.push([`title`,c])}return e.pos=a,e.posMax=f,!0}var vn=/^([a-zA-Z0-9.!#$%&'*+/=?^_`{|}~-]+@[a-zA-Z0-9](?:[a-zA-Z0-9-]{0,61}[a-zA-Z0-9])?(?:\.[a-zA-Z0-9](?:[a-zA-Z0-9-]{0,61}[a-zA-Z0-9])?)*)$/,yn=/^([a-zA-Z][a-zA-Z0-9+.-]{1,31}):([^<>\x00-\x20]*)$/;function bn(e,t){let n=e.pos;if(e.src.charCodeAt(n)!==60)return!1;let r=e.pos,i=e.posMax;for(;;){if(++n>=i)return!1;let t=e.src.charCodeAt(n);if(t===60)return!1;if(t===62)break}let a=e.src.slice(r+1,n);if(yn.test(a)){let n=e.md.normalizeLink(a);if(!e.md.validateLink(n))return!1;if(!t){let t=e.push(`link_open`,`a`,1);t.attrs=[[`href`,n]],t.markup=`autolink`,t.info=`auto`;let r=e.push(`text`,``,0);r.content=e.md.normalizeLinkText(a);let i=e.push(`link_close`,`a`,-1);i.markup=`autolink`,i.info=`auto`}return e.pos+=a.length+2,!0}if(vn.test(a)){let n=e.md.normalizeLink(`mailto:`+a);if(!e.md.validateLink(n))return!1;if(!t){let t=e.push(`link_open`,`a`,1);t.attrs=[[`href`,n]],t.markup=`autolink`,t.info=`auto`;let r=e.push(`text`,``,0);r.content=e.md.normalizeLinkText(a);let i=e.push(`link_close`,`a`,-1);i.markup=`autolink`,i.info=`auto`}return e.pos+=a.length+2,!0}return!1}function xn(e){return/^<a[>\s]/i.test(e)}function Sn(e){return/^<\/a\s*>/i.test(e)}function Cn(e){let t=e|32;return t>=97&&t<=122}function wn(e,t){if(!e.md.options.html)return!1;let n=e.posMax,r=e.pos;if(e.src.charCodeAt(r)!==60||r+2>=n)return!1;let i=e.src.charCodeAt(r+1);if(i!==33&&i!==63&&i!==47&&!Cn(i))return!1;let a=e.src.slice(r).match(qt);if(!a)return!1;if(!t){let t=e.push(`html_inline`,``,0);t.content=a[0],xn(t.content)&&e.linkLevel++,Sn(t.content)&&e.linkLevel--}return e.pos+=a[0].length,!0}var Tn=/^&#((?:x[a-f0-9]{1,6}|[0-9]{1,7}));/i,En=/^&([a-z][a-z0-9]{1,31});/i;function Dn(e,t){let n=e.pos,r=e.posMax;if(e.src.charCodeAt(n)!==38||n+1>=r)return!1;if(e.src.charCodeAt(n+1)===35){let r=e.src.slice(n).match(Tn);if(r){if(!t){let t=r[1][0].toLowerCase()===`x`?parseInt(r[1].slice(1),16):parseInt(r[1],10),n=e.push(`text_special`,``,0);n.content=He(t)?O(t):O(65533),n.markup=r[0],n.info=`entity`}return e.pos+=r[0].length,!0}}else{let r=e.src.slice(n).match(En);if(r){let n=Pe(r[0]);if(n!==r[0]){if(!t){let t=e.push(`text_special`,``,0);t.content=n,t.markup=r[0],t.info=`entity`}return e.pos+=r[0].length,!0}}}return!1}function On(e){let t={},n=e.length;if(!n)return;let r=0,i=-2,a=[];for(let o=0;o<n;o++){let n=e[o];if(a.push(0),(e[r].marker!==n.marker||i!==n.token-1)&&(r=o),i=n.token,n.length=n.length||0,!n.close)continue;t.hasOwnProperty(n.marker)||(t[n.marker]=[-1,-1,-1,-1,-1,-1]);let s=t[n.marker][(n.open?3:0)+n.length%3],c=r-a[r]-1,l=c;for(;c>s;c-=a[c]+1){let t=e[c];if(t.marker===n.marker&&t.open&&t.end<0){let r=!1;if((t.close||n.open)&&(t.length+n.length)%3==0&&(t.length%3!=0||n.length%3!=0)&&(r=!0),!r){let r=c>0&&!e[c-1].open?a[c-1]+1:0;a[o]=o-c+r,a[c]=r,n.open=!1,t.end=o,t.close=!1,l=-1,i=-2;break}}}l!==-1&&(t[n.marker][(n.open?3:0)+(n.length||0)%3]=l)}}function kn(e){let t=e.tokens_meta,n=e.tokens_meta.length;On(e.delimiters);for(let e=0;e<n;e++)t[e]&&t[e].delimiters&&On(t[e].delimiters)}function An(e){let t,n,r=0,i=e.tokens,a=e.tokens.length;for(t=n=0;t<a;t++)i[t].nesting<0&&r--,i[t].level=r,i[t].nesting>0&&r++,i[t].type===`text`&&t+1<a&&i[t+1].type===`text`?i[t+1].content=i[t].content+i[t+1].content:(t!==n&&(i[n]=i[t]),n++);t!==n&&(i.length=n)}var jn=[[`text`,en],[`linkify`,nn],[`newline`,rn],[`escape`,on],[`backticks`,sn],[`strikethrough`,dn.tokenize],[`emphasis`,hn.tokenize],[`link`,gn],[`image`,_n],[`autolink`,bn],[`html_inline`,wn],[`entity`,Dn]],Mn=[[`balance_pairs`,kn],[`strikethrough`,dn.postProcess],[`emphasis`,hn.postProcess],[`fragments_join`,An]];function q(){this.ruler=new z;for(let e=0;e<jn.length;e++)this.ruler.push(jn[e][0],jn[e][1]);this.ruler2=new z;for(let e=0;e<Mn.length;e++)this.ruler2.push(Mn[e][0],Mn[e][1])}q.prototype.skipToken=function(e){let t=e.pos,n=this.ruler.getRules(``),r=n.length,i=e.md.options.maxNesting,a=e.cache;if(a[t]!==void 0){e.pos=a[t];return}let o=!1;if(e.level<i){for(let i=0;i<r;i++)if(e.level++,o=n[i](e,!0),e.level--,o){if(t>=e.pos)throw Error(`inline rule didn't increment state.pos`);break}}else e.pos=e.posMax;o||e.pos++,a[t]=e.pos},q.prototype.tokenize=function(e){let t=this.ruler.getRules(``),n=t.length,r=e.posMax,i=e.md.options.maxNesting;for(;e.pos<r;){let a=e.pos,o=!1;if(e.level<i){for(let r=0;r<n;r++)if(o=t[r](e,!1),o){if(a>=e.pos)throw Error(`inline rule didn't increment state.pos`);break}}if(o){if(e.pos>=r)break;continue}e.pending+=e.src[e.pos++]}e.pending&&e.pushPending()},q.prototype.parse=function(e,t,n,r){let i=new this.State(e,t,n,r);this.tokenize(i);let a=this.ruler2.getRules(``),o=a.length;for(let e=0;e<o;e++)a[e](i)},q.prototype.State=K;function Nn(e){let t={};e||={},t.src_Any=fe.source,t.src_Cc=pe.source,t.src_Z=_e.source,t.src_P=he.source,t.src_ZPCc=[t.src_Z,t.src_P,t.src_Cc].join(`|`),t.src_ZCc=[t.src_Z,t.src_Cc].join(`|`);let n=`[><｜]`;return t.src_pseudo_letter=`(?:(?!${n}|${t.src_ZPCc})${t.src_Any})`,t.src_ip4=`(?:(25[0-5]|2[0-4][0-9]|[01]?[0-9][0-9]?)\\.){3}(25[0-5]|2[0-4][0-9]|[01]?[0-9][0-9]?)`,t.src_auth=`(?:(?:(?!${t.src_ZCc}|[@/\\[\\]()]).){1,50}@)?`,t.src_port=`(?::(?:6(?:[0-4]\\d{3}|5(?:[0-4]\\d{2}|5(?:[0-2]\\d|3[0-5])))|[1-5]?\\d{1,4}))?`,t.src_host_terminator=`(?=$|${n}|${t.src_ZPCc})(?!${e[`---`]?`-(?!--)|`:`-|`}_|:\\d|\\.-|\\.(?!$|${t.src_ZPCc}))`,t.src_path=`(?:[/?#](?:(?!${t.src_ZCc}|${n}|[()[\\]{}.,"'?!\\-;]).|\\[(?:(?!${t.src_ZCc}|\\]).)*\\]|\\((?:(?!${t.src_ZCc}|[)]).)*\\)|\\{(?:(?!${t.src_ZCc}|[}]).)*\\}|\\"(?:(?!${t.src_ZCc}|["]).)+\\"|\\'(?:(?!${t.src_ZCc}|[']).)+\\'|\\'(?=${t.src_pseudo_letter}|[-])|\\.{2,}[a-zA-Z0-9%/&]|\\.(?!${t.src_ZCc}|[.]|$)|`+(e[`---`]?`\\-(?!--(?:[^-]|$))(?:-*)|`:`\\-+|`)+`,(?!${t.src_ZCc}|$)|;(?!${t.src_ZCc}|$)|\\!+(?!${t.src_ZCc}|[!]|$)|\\?(?!${t.src_ZCc}|[?]|$))+|\\/)?`,t.src_email_name=`[\\-;:&=\\+\\$,\\.a-zA-Z0-9_][\\-;:&=\\+\\$,\\"\\.a-zA-Z0-9_]{0,63}`,t.src_xn=`xn--[a-z0-9\\-]{1,59}`,t.src_domain_root=`(?:`+t.src_xn+`|${t.src_pseudo_letter}{1,63})`,t.src_domain=`(?:`+t.src_xn+`|(?:${t.src_pseudo_letter})|(?:${t.src_pseudo_letter}(?:-|${t.src_pseudo_letter}){0,61}${t.src_pseudo_letter}))`,t.src_host=`(?:(?:(?:(?:${t.src_domain})\\.)*${t.src_domain}))`,t.tpl_host_fuzzy=`(?:`+t.src_ip4+`|(?:(?:(?:${t.src_domain})\\.)+(?:%TLDS%)))`,t.tpl_host_no_ip_fuzzy=`(?:(?:(?:${t.src_domain})\\.)+(?:%TLDS%))`,t.src_host_strict=t.src_host+t.src_host_terminator,t.tpl_host_fuzzy_strict=t.tpl_host_fuzzy+t.src_host_terminator,t.src_host_port_strict=t.src_host+t.src_port+t.src_host_terminator,t.tpl_host_port_fuzzy_strict=t.tpl_host_fuzzy+t.src_port+t.src_host_terminator,t.tpl_host_port_no_ip_fuzzy_strict=t.tpl_host_no_ip_fuzzy+t.src_port+t.src_host_terminator,t.tpl_host_fuzzy_test=`localhost|www\\.|\\.\\d{1,3}\\.|(?:\\.(?:%TLDS%)(?:${t.src_ZPCc}|>|$))`,t.tpl_email_fuzzy=`(^|${n}|"|\\(|${t.src_ZCc})(${t.src_email_name}@${t.tpl_host_fuzzy_strict})`,t.tpl_link_fuzzy=`(^|(?![.:/\\-_@])(?:[$+<=>^\`|\uff5c]|${t.src_ZPCc}))((?![$+<=>^\`|\uff5c])${t.tpl_host_port_fuzzy_strict}${t.src_path})`,t.tpl_link_no_ip_fuzzy=`(^|(?![.:/\\-_@])(?:[$+<=>^\`|\uff5c]|${t.src_ZPCc}))((?![$+<=>^\`|\uff5c])${t.tpl_host_port_no_ip_fuzzy_strict}${t.src_path})`,t}function Pn(e){return Array.prototype.slice.call(arguments,1).forEach(function(t){t&&Object.keys(t).forEach(function(n){e[n]=t[n]})}),e}function Fn(e){return Object.prototype.toString.call(e)}function In(e){return Fn(e)===`[object String]`}function Ln(e){return Fn(e)===`[object Object]`}function Rn(e){return Fn(e)===`[object RegExp]`}function zn(e){return Fn(e)===`[object Function]`}function Bn(e){return e.replace(/[.?*+^$[\]\\(){}|-]/g,`\\$&`)}var Vn={fuzzyLink:!0,fuzzyEmail:!0,fuzzyIP:!1};function Hn(e){return Object.keys(e||{}).reduce(function(e,t){return e||Vn.hasOwnProperty(t)},!1)}var Un={"http:":{validate:function(e,t,n){let r=e.slice(t);return n.re.http||(n.re.http=RegExp(`^\\/\\/${n.re.src_auth}${n.re.src_host_port_strict}${n.re.src_path}`,`i`)),n.re.http.test(r)?r.match(n.re.http)[0].length:0}},"https:":`http:`,"ftp:":`http:`,"//":{validate:function(e,t,n){let r=e.slice(t);return n.re.no_http||(n.re.no_http=RegExp(`^`+n.re.src_auth+`(?:localhost|(?:(?:${n.re.src_domain})\\.)+${n.re.src_domain_root})`+n.re.src_port+n.re.src_host_terminator+n.re.src_path,`i`)),n.re.no_http.test(r)?t>=3&&e[t-3]===`:`||t>=3&&e[t-3]===`/`?0:r.match(n.re.no_http)[0].length:0}},"mailto:":{validate:function(e,t,n){let r=e.slice(t);return n.re.mailto||(n.re.mailto=RegExp(`^${n.re.src_email_name}@${n.re.src_host_strict}`,`i`)),n.re.mailto.test(r)?r.match(n.re.mailto)[0].length:0}}},Wn=`a[cdefgilmnoqrstuwxz]|b[abdefghijmnorstvwyz]|c[acdfghiklmnoruvwxyz]|d[ejkmoz]|e[cegrstu]|f[ijkmor]|g[abdefghilmnpqrstuwy]|h[kmnrtu]|i[delmnoqrst]|j[emop]|k[eghimnprwyz]|l[abcikrstuvy]|m[acdeghklmnopqrstuvwxyz]|n[acefgilopruz]|om|p[aefghklmnrstwy]|qa|r[eosuw]|s[abcdeghijklmnortuvxyz]|t[cdfghjklmnortvwz]|u[agksyz]|v[aceginu]|w[fs]|y[et]|z[amw]`,Gn=`biz|com|edu|gov|net|org|pro|web|xxx|aero|asia|coop|info|museum|name|shop|рф`.split(`|`);function Kn(e){return function(t,n){let r=t.slice(n);return e.test(r)?r.match(e)[0].length:0}}function qn(){return function(e,t){t.normalize(e)}}function Jn(e){let t=e.re=Nn(e.__opts__),n=e.__tlds__.slice();e.onCompile(),e.__tlds_replaced__||n.push(Wn),n.push(t.src_xn),t.src_tlds=n.join(`|`);function r(e){return e.replace(`%TLDS%`,t.src_tlds)}t.email_fuzzy=RegExp(r(t.tpl_email_fuzzy),`i`),t.email_fuzzy_global=RegExp(r(t.tpl_email_fuzzy),`ig`),t.link_fuzzy=RegExp(r(t.tpl_link_fuzzy),`i`),t.link_fuzzy_global=RegExp(r(t.tpl_link_fuzzy),`ig`),t.link_no_ip_fuzzy=RegExp(r(t.tpl_link_no_ip_fuzzy),`i`),t.link_no_ip_fuzzy_global=RegExp(r(t.tpl_link_no_ip_fuzzy),`ig`),t.host_fuzzy_test=RegExp(r(t.tpl_host_fuzzy_test),`i`);let i=[];e.__compiled__={};function a(e,t){throw Error(`(LinkifyIt) Invalid schema "${e}": ${t}`)}Object.keys(e.__schemas__).forEach(function(t){let n=e.__schemas__[t];if(n===null)return;let r={validate:null,link:null};if(e.__compiled__[t]=r,Ln(n)){Rn(n.validate)?r.validate=Kn(n.validate):zn(n.validate)?r.validate=n.validate:a(t,n),zn(n.normalize)?r.normalize=n.normalize:n.normalize?a(t,n):r.normalize=qn();return}if(In(n)){i.push(t);return}a(t,n)}),i.forEach(function(t){e.__compiled__[e.__schemas__[t]]&&(e.__compiled__[t].validate=e.__compiled__[e.__schemas__[t]].validate,e.__compiled__[t].normalize=e.__compiled__[e.__schemas__[t]].normalize)}),e.__compiled__[``]={validate:null,normalize:qn()};let o=Object.keys(e.__compiled__).filter(function(t){return t.length>0&&e.__compiled__[t]}).map(Bn).join(`|`);e.re.schema_test=RegExp(`(^|(?!_)(?:[><\uff5c]|${t.src_ZPCc}))(${o})`,`i`),e.re.schema_search=RegExp(`(^|(?!_)(?:[><\uff5c]|${t.src_ZPCc}))(${o})`,`ig`),e.re.schema_at_start=RegExp(`^${e.re.schema_search.source}`,`i`),e.re.pretest=RegExp(`(${e.re.schema_test.source})|(${e.re.host_fuzzy_test.source})|@`,`i`)}function Yn(e,t,n,r){let i=e.slice(n,r);this.schema=t.toLowerCase(),this.index=n,this.lastIndex=r,this.raw=i,this.text=i,this.url=i}function J(e,t){if(!(this instanceof J))return new J(e,t);t||Hn(e)&&(t=e,e={}),this.__opts__=Pn({},Vn,t),this.__schemas__=Pn({},Un,e),this.__compiled__={},this.__tlds__=Gn,this.__tlds_replaced__=!1,this.re={},Jn(this)}J.prototype.add=function(e,t){return this.__schemas__[e]=t,Jn(this),this},J.prototype.set=function(e){return this.__opts__=Pn(this.__opts__,e),this},J.prototype.test=function(e){if(!e.length)return!1;let t,n;if(this.re.schema_test.test(e)){for(n=this.re.schema_search,n.lastIndex=0;(t=n.exec(e))!==null;)if(this.testSchemaAt(e,t[2],n.lastIndex))return!0}return!!(this.__opts__.fuzzyLink&&this.__compiled__[`http:`]&&e.search(this.re.host_fuzzy_test)>=0&&e.match(this.__opts__.fuzzyIP?this.re.link_fuzzy:this.re.link_no_ip_fuzzy)!==null||this.__opts__.fuzzyEmail&&this.__compiled__[`mailto:`]&&e.indexOf(`@`)>=0&&e.match(this.re.email_fuzzy)!==null)},J.prototype.pretest=function(e){return this.re.pretest.test(e)},J.prototype.testSchemaAt=function(e,t,n){return this.__compiled__[t.toLowerCase()]?this.__compiled__[t.toLowerCase()].validate(e,n,this):0},J.prototype.match=function(e){let t=[],n=[],r=[],i=[],a,o,s;function c(e,t){return e?t?e.index===t.index?e.lastIndex>=t.lastIndex?e:t:e.index<t.index?e:t:e:t}if(!e.length)return null;if(this.re.schema_test.test(e))for(s=this.re.schema_search,s.lastIndex=0;(a=s.exec(e))!==null;)o=this.testSchemaAt(e,a[2],s.lastIndex),o&&n.push({schema:a[2],index:a.index+a[1].length,lastIndex:a.index+a[0].length+o});if(this.__opts__.fuzzyLink&&this.__compiled__[`http:`])for(s=this.__opts__.fuzzyIP?this.re.link_fuzzy_global:this.re.link_no_ip_fuzzy_global,s.lastIndex=0;(a=s.exec(e))!==null;)r.push({schema:``,index:a.index+a[1].length,lastIndex:a.index+a[0].length});if(this.__opts__.fuzzyEmail&&this.__compiled__[`mailto:`])for(s=this.re.email_fuzzy_global,s.lastIndex=0;(a=s.exec(e))!==null;)i.push({schema:`mailto:`,index:a.index+a[1].length,lastIndex:a.index+a[0].length});let l=[0,0,0],u=0;for(;;){let a=[n[l[0]],i[l[1]],r[l[2]]],o=c(c(a[0],a[1]),a[2]);if(!o)break;if(o===a[0]?l[0]++:o===a[1]?l[1]++:l[2]++,o.index<u)continue;let s=new Yn(e,o.schema,o.index,o.lastIndex);this.__compiled__[s.schema].normalize(s,this),t.push(s),u=o.lastIndex}return t.length?t:null},J.prototype.matchAtStart=function(e){if(!e.length)return null;let t=this.re.schema_at_start.exec(e);if(!t)return null;let n=this.testSchemaAt(e,t[2],t[0].length);if(!n)return null;let r=new Yn(e,t[2],t.index+t[1].length,t.index+t[0].length+n);return this.__compiled__[r.schema].normalize(r,this),r},J.prototype.tlds=function(e,t){return e=Array.isArray(e)?e:[e],t?(this.__tlds__=this.__tlds__.concat(e).sort().filter(function(e,t,n){return e!==n[t-1]}).reverse(),Jn(this),this):(this.__tlds__=e.slice(),this.__tlds_replaced__=!0,Jn(this),this)},J.prototype.normalize=function(e){e.schema||(e.url=`http://${e.url}`),e.schema===`mailto:`&&!/^mailto:/i.test(e.url)&&(e.url=`mailto:${e.url}`)},J.prototype.onCompile=function(){};var Y=2147483647,X=36,Xn=1,Zn=26,Qn=38,$n=700,er=72,tr=128,nr=`-`,rr=/^xn--/,ir=/[^\0-\x7F]/,ar=/[\x2E\u3002\uFF0E\uFF61]/g,or={overflow:`Overflow: input needs wider integers to process`,"not-basic":`Illegal input >= 0x80 (not a basic code point)`,"invalid-input":`Invalid input`},sr=X-Xn,Z=Math.floor,cr=String.fromCharCode;function Q(e){throw RangeError(or[e])}function lr(e,t){let n=[],r=e.length;for(;r--;)n[r]=t(e[r]);return n}function ur(e,t){let n=e.split(`@`),r=``;n.length>1&&(r=n[0]+`@`,e=n[1]),e=e.replace(ar,`.`);let i=lr(e.split(`.`),t).join(`.`);return r+i}function dr(e){let t=[],n=0,r=e.length;for(;n<r;){let i=e.charCodeAt(n++);if(i>=55296&&i<=56319&&n<r){let r=e.charCodeAt(n++);(r&64512)==56320?t.push(((i&1023)<<10)+(r&1023)+65536):(t.push(i),n--)}else t.push(i)}return t}var fr=e=>String.fromCodePoint(...e),pr=function(e){return e>=48&&e<58?26+(e-48):e>=65&&e<91?e-65:e>=97&&e<123?e-97:X},mr=function(e,t){return e+22+75*(e<26)-((t!=0)<<5)},hr=function(e,t,n){let r=0;for(e=n?Z(e/$n):e>>1,e+=Z(e/t);e>455;r+=X)e=Z(e/sr);return Z(r+36*e/(e+Qn))},gr=function(e){let t=[],n=e.length,r=0,i=tr,a=er,o=e.lastIndexOf(nr);o<0&&(o=0);for(let n=0;n<o;++n)e.charCodeAt(n)>=128&&Q(`not-basic`),t.push(e.charCodeAt(n));for(let s=o>0?o+1:0;s<n;){let o=r;for(let t=1,i=X;;i+=X){s>=n&&Q(`invalid-input`);let o=pr(e.charCodeAt(s++));o>=X&&Q(`invalid-input`),o>Z((Y-r)/t)&&Q(`overflow`),r+=o*t;let c=i<=a?Xn:i>=a+Zn?Zn:i-a;if(o<c)break;let l=X-c;t>Z(Y/l)&&Q(`overflow`),t*=l}let c=t.length+1;a=hr(r-o,c,o==0),Z(r/c)>Y-i&&Q(`overflow`),i+=Z(r/c),r%=c,t.splice(r++,0,i)}return String.fromCodePoint(...t)},_r=function(e){let t=[];e=dr(e);let n=e.length,r=tr,i=0,a=er;for(let n of e)n<128&&t.push(cr(n));let o=t.length,s=o;for(o&&t.push(nr);s<n;){let n=Y;for(let t of e)t>=r&&t<n&&(n=t);let c=s+1;n-r>Z((Y-i)/c)&&Q(`overflow`),i+=(n-r)*c,r=n;for(let n of e)if(n<r&&++i>Y&&Q(`overflow`),n===r){let e=i;for(let n=X;;n+=X){let r=n<=a?Xn:n>=a+Zn?Zn:n-a;if(e<r)break;let i=e-r,o=X-r;t.push(cr(mr(r+i%o,0))),e=Z(i/o)}t.push(cr(mr(e,0))),a=hr(i,c,s===o),i=0,++s}++i,++r}return t.join(``)},vr={version:`2.3.1`,ucs2:{decode:dr,encode:fr},decode:gr,encode:_r,toASCII:function(e){return ur(e,function(e){return ir.test(e)?`xn--`+_r(e):e})},toUnicode:function(e){return ur(e,function(e){return rr.test(e)?gr(e.slice(4).toLowerCase()):e})}},yr={default:{options:{html:!1,xhtmlOut:!1,breaks:!1,langPrefix:`language-`,linkify:!1,typographer:!1,quotes:`“”‘’`,highlight:null,maxNesting:100},components:{core:{},block:{},inline:{}}},zero:{options:{html:!1,xhtmlOut:!1,breaks:!1,langPrefix:`language-`,linkify:!1,typographer:!1,quotes:`“”‘’`,highlight:null,maxNesting:20},components:{core:{rules:[`normalize`,`block`,`inline`,`text_join`]},block:{rules:[`paragraph`]},inline:{rules:[`text`],rules2:[`balance_pairs`,`fragments_join`]}}},commonmark:{options:{html:!0,xhtmlOut:!0,breaks:!1,langPrefix:`language-`,linkify:!1,typographer:!1,quotes:`“”‘’`,highlight:null,maxNesting:20},components:{core:{rules:[`normalize`,`block`,`inline`,`text_join`]},block:{rules:[`blockquote`,`code`,`fence`,`heading`,`hr`,`html_block`,`lheading`,`list`,`reference`,`paragraph`]},inline:{rules:[`autolink`,`backticks`,`emphasis`,`entity`,`escape`,`html_inline`,`image`,`link`,`newline`,`text`],rules2:[`balance_pairs`,`emphasis`,`fragments_join`]}}}},br=/^(vbscript|javascript|file|data):/,xr=/^data:image\/(gif|png|jpeg|webp);/;function Sr(e){let t=e.trim().toLowerCase();return!br.test(t)||xr.test(t)}var Cr=[`http:`,`https:`,`mailto:`];function wr(e){let t=ue(e,!0);if(t.hostname&&(!t.protocol||Cr.indexOf(t.protocol)>=0))try{t.hostname=vr.toASCII(t.hostname)}catch{}return x(S(t))}function Tr(e){let t=ue(e,!0);if(t.hostname&&(!t.protocol||Cr.indexOf(t.protocol)>=0))try{t.hostname=vr.toUnicode(t.hostname)}catch{}return v(S(t),v.defaultChars+`%`)}function $(e,t){if(!(this instanceof $))return new $(e,t);t||Le(e)||(t=e||{},e=`default`),this.inline=new q,this.block=new G,this.core=new Mt,this.renderer=new R,this.linkify=new J,this.validateLink=Sr,this.normalizeLink=wr,this.normalizeLinkText=Tr,this.utils=Fe,this.helpers=Be({},ot),this.options={},this.configure(e),t&&this.set(t)}$.prototype.set=function(e){return Be(this.options,e),this},$.prototype.configure=function(e){let t=this;if(Le(e)){let t=e;if(e=yr[t],!e)throw Error('Wrong `markdown-it` preset "'+t+`", check name`)}if(!e)throw Error("Wrong `markdown-it` preset, can't be empty");return e.options&&t.set(e.options),e.components&&Object.keys(e.components).forEach(function(n){e.components[n].rules&&t[n].ruler.enableOnly(e.components[n].rules),e.components[n].rules2&&t[n].ruler2.enableOnly(e.components[n].rules2)}),this},$.prototype.enable=function(e,t){let n=[];Array.isArray(e)||(e=[e]),[`core`,`block`,`inline`].forEach(function(t){n=n.concat(this[t].ruler.enable(e,!0))},this),n=n.concat(this.inline.ruler2.enable(e,!0));let r=e.filter(function(e){return n.indexOf(e)<0});if(r.length&&!t)throw Error(`MarkdownIt. Failed to enable unknown rule(s): `+r);return this},$.prototype.disable=function(e,t){let n=[];Array.isArray(e)||(e=[e]),[`core`,`block`,`inline`].forEach(function(t){n=n.concat(this[t].ruler.disable(e,!0))},this),n=n.concat(this.inline.ruler2.disable(e,!0));let r=e.filter(function(e){return n.indexOf(e)<0});if(r.length&&!t)throw Error(`MarkdownIt. Failed to disable unknown rule(s): `+r);return this},$.prototype.use=function(e){let t=[this].concat(Array.prototype.slice.call(arguments,1));return e.apply(e,t),this},$.prototype.parse=function(e,t){if(typeof e!=`string`)throw Error(`Input data should be a String`);let n=new this.core.State(e,this,t);return this.core.process(n),n.tokens},$.prototype.render=function(e,t){return t||={},this.renderer.render(this.parse(e,t),this.options,t)},$.prototype.parseInline=function(e,t){let n=new this.core.State(e,this,t);return n.inlineMode=!0,this.core.process(n),n.tokens},$.prototype.renderInline=function(e,t){return t||={},this.renderer.render(this.parseInline(e,t),this.options,t)};var Er=`# AuroraMihomo 使用文档\r
\r
面向使用者的完整说明：每个页面能做什么、字段怎么填、有哪些坑。\r
\r
## 目录\r
\r
- [概念与数据流](#概念与数据流)\r
- [登录](#登录)\r
- [控制台](#控制台)\r
- [内核管理](#内核管理)\r
- [Sub-Store 管理](#sub-store-管理)\r
  - [单个订阅](#单个订阅)\r
  - [处理管道与 12 个算子](#处理管道与-12-个算子)\r
  - [组合订阅](#组合订阅)\r
  - [模板文件](#模板文件)\r
  - [分享管理](#分享管理)\r
- [配置中心](#配置中心)\r
  - [远程订阅来源](#远程订阅来源)\r
  - [本地基础配置 11 组](#本地基础配置-11-组)\r
  - [合并策略与冲突](#合并策略与冲突)\r
- [配置差异](#配置差异)\r
- [运行日志](#运行日志)\r
- [系统设置](#系统设置)\r
- [Zashboard 面板](#zashboard-面板)\r
- [配置文件与环境变量](#配置文件与环境变量)\r
- [常见问题](#常见问题)\r
\r
---\r
\r
## 概念与数据流\r
\r
最终交给 mihomo 内核的 \`config.yaml\` 由三层合并而成：\r
\r
\`\`\`\r
Base（本地基础配置）      你在「配置中心」填的内容\r
        +\r
Remote（远程层）          订阅 / 组合 / 文件模板 / 外部链接，二选一\r
        +\r
Override（覆写层）        模板文件里的 YAML 覆写或 JS 脚本\r
        ↓\r
   冲突检测与策略裁决\r
        ↓\r
   data/config.yaml  →  mihomo 内核\r
\`\`\`\r
\r
几个容易混淆的点：\r
\r
- **订阅的「刷新缓存」不会改最终配置**。它只更新该订阅自己的节点缓存，供分享链接与预览使用。要让改动进内核，得去配置中心点「保存并应用」或「立即拉取并合并」。\r
- **分享链接与最终配置是两条独立的路**。分享链接实时渲染订阅/组合的节点，不经过三层合并；最终配置才是内核加载的东西。\r
- **合并前自动备份**。每次合并都会把旧配置存进 \`data/backups/\`（保留 10 份），校验失败自动回滚。\r
\r
---\r
\r
## 登录\r
\r
只有一个密码框。初始密码在首次启动时生成：\r
\r
\`\`\`bash\r
cat data/initial_password.txt                        # 二进制部署\r
docker exec auroramihomo cat /data/initial_password.txt   # Docker\r
\`\`\`\r
\r
登录后请立即在「系统设置 → 管理员密码」改掉，并手工删除该文件——它不会自动删除。\r
\r
5 分钟内失败 5 次会锁定 15 分钟，页面会显示剩余时间。\r
\r
---\r
\r
## 控制台\r
\r
只读概览页，五张状态卡：\r
\r
| 卡片 | 含义 |\r
|---|---|\r
| 内核状态 | 运行中 / 已停止 |\r
| 内核版本 | 由 \`mihomo -v\` 探测 |\r
| 代理节点数 | 第一个组合订阅渲染出的节点数 |\r
| 上次订阅更新 | 所有订阅中最近一次刷新时间 |\r
| 配置状态 | 正常 / 未配置；有未解决冲突时额外显示条数 |\r
\r
下方是后台任务列表（配置合并、内核重载、版本检查）与实时内核日志（最多 100 行）。\r
\r
右上角徽标显示实时通道状态：连接中 / 已连接 / 已断开 / 连接异常 / 服务端重启中。它走 WebSocket，断线会自动指数退避重连。\r
\r
> 「N 项冲突待处理」目前只是提示，**界面上没有逐条解决冲突的入口**。详见[合并策略与冲突](#合并策略与冲突)。\r
\r
---\r
\r
## 内核管理\r
\r
五个操作：\r
\r
| 按钮 | 作用 | 是否二次确认 |\r
|---|---|---|\r
| 启动 | 拉起 mihomo 进程 | 否 |\r
| 停止 | 结束进程 | 是（会中断所有代理连接） |\r
| 重启 | 停止后重新拉起 | 是 |\r
| 重载配置 | 让内核重新读 \`config.yaml\`，不重启进程 | 否 |\r
| 更新内核版本 | 下载最新 mihomo 并替换 | 否（设置页的同名操作有确认） |\r
\r
改完配置后用「重载配置」通常就够了，比重启温和。少数配置项内核只在启动时读，必须「重启」才生效：\r
\r
- **TUN 相关**\r
- **\`external-controller\`**（外部控制监听地址）。改完保存并合并后，\`config.yaml\` 里已经是新值，但内核仍监听在旧地址上——热重载走的是内核自己的配置更新接口，而这个字段不在它支持热更新的范围内。改这一项后记得点一次「重启」，否则会以为改了没生效（内置 Zashboard 也会因为连不上旧端口而打不开）。\r
\r
---\r
\r
## Sub-Store 管理\r
\r
四个标签页：单个订阅、组合订阅、模板文件、分享管理。\r
\r
### 单个订阅\r
\r
**新建时要填什么**\r
\r
| 字段 | 必填 | 说明 |\r
|---|---|---|\r
| 订阅名称 | 是 | 如「机场A」 |\r
| 自定义 User-Agent | 否 | 部分上游按 UA 返回不同格式，如 \`ClashForWindows/0.20.39\` |\r
| 节点来源 | — | 二选一：远程订阅地址 / 手动粘贴节点 |\r
| 订阅地址 | 远程模式必填 | \`https://...\` |\r
| 节点内容 | 粘贴模式必填 | 每行一条分享链接（\`ss://\` \`vmess://\` \`vless://\` \`trojan://\` \`hysteria2://\` 等），也可直接粘 Base64 订阅或 Clash YAML |\r
| 启用此订阅 | — | 默认勾选。禁用后不参与组合与配置合并，分享链接也失效 |\r
| 独立处理管道 | 否 | 见下节。先于组合管道执行 |\r
\r
手动粘贴的节点无需回源，保存即可用。\r
\r
**列表上能做什么**\r
\r
- **复制外部分享链接**：名称下方的下拉，15 种格式任选，点哪个复制哪个\r
- **预览**：看处理前后的节点对比，不写库\r
- **刷新缓存**：回源拉最新节点。**只更新缓存，不改最终配置**\r
- **分享设置**：改名、设有效期、重置凭据、撤销\r
- **删除**：会二次确认\r
\r
流量列显示「已用 / 总量」与进度条（≥70% 转琥珀、≥90% 转红），下方是到期时间与剩余天数。这些信息来自上游返回的响应头，不是所有机场都提供。\r
\r
> 「自动更新间隔」字段已从界面移除。订阅不再各自定时回源，刷新时机统一由配置中心的定时拉取或手动操作决定。\r
\r
### 处理管道与 12 个算子\r
\r
管道按从上到下的顺序执行。订阅自己的管道先跑，然后才是组合的管道。\r
\r
| 算子 | 参数 | 作用 |\r
|---|---|---|\r
| **常用配置** | 9 个下拉，默认都是「不改」 | 批量改节点属性：过滤非法节点、UDP 转发、跳过证书验证、TCP Fast Open、VMess AEAD、连接复用、阻止 QUIC、ECN、IP 版本。不适用于当前协议的选项自动跳过 |\r
| **节点过滤** | 保留/剔除 + 正则 | 按节点名正则整个保留或剔除。如 \`HK\\|HongKong\` |\r
| **正则改名** | 匹配 + 替换 | 正则替换节点名 |\r
| **自动加国旗** | 无 | 按名字给节点加国旗 emoji。支持 HK TW JP SG KR US UK DE FR CA AU RU IN TR NL |\r
| **覆盖节点属性** | 键 + 值 + 值类型 | 逐节点写入属性，如 \`udp=true\`、\`skip-cert-verify=true\`。值类型可选文本/布尔/数字 |\r
| **按地区筛选** | 保留/剔除 + 地区列表 | 每行一个地区码，自动识别中英文与旗帜 emoji |\r
| **剔除无效节点** | 无 | 自动剔除「剩余流量」「到期时间」「官网」这类机场信息假节点 |\r
| **正则删除字符** | 匹配 | 只删名称里的匹配片段，不删节点本身。如 \`【[^】]*】\` |\r
| **名称排序** | 升序/降序 | 按名称排序 |\r
| **关键词排序** | 关键词列表 | 按关键词优先级排，未命中的按名称排到末尾 |\r
| **域名解析为 IP** | 可选优先 IPv6 | 把节点域名解析成 IP。解析失败保留原域名，不中断管道。超时 3 秒 |\r
| **⚡ 注入 JS 脚本** | JS 代码 | 传入 \`proxies\` 数组，返回修改后的数组。执行超时 5 秒 |\r
\r
JS 脚本的骨架：\r
\r
\`\`\`javascript\r
function operator(proxies) {\r
  // 在这里修改 proxies\r
  return proxies\r
}\r
\`\`\`\r
\r
它能力最强也最容易出错，界面上用实色按钮与其他算子区分。\r
\r
### 组合订阅\r
\r
把多条订阅聚合成一个，再统一处理。\r
\r
| 字段 | 必填 | 说明 |\r
|---|---|---|\r
| 组合名称 | 是 | |\r
| 选择基础订阅源 | 至少一个 | 复选，点击切换 |\r
| 处理管道 | 否 | 同上 12 个算子 |\r
| 启用 | — | 默认勾选。关闭后分享链接立即失效 |\r
\r
组合本身没有输出格式设置——格式由分享链接的 \`?target=\` 决定。\r
\r
**15 种输出格式**：默认（Mihomo YAML）、Clash / Mihomo、Base64 订阅、明文分享链接、Surge、Surge Mac、Loon、QuantumultX、sing-box、V2Ray、纯 JSON、Stash、Surfboard、Shadowrocket、Egern。\r
\r
> 分享设置弹窗里的格式下拉只有 10 项，是常用子集。要用全部 15 种，走列表页的「复制外部分享链接」下拉，或手工在链接后加 \`?target=\`。\r
\r
### 模板文件\r
\r
两种用途，由「配置类型」决定：\r
\r
**① 文件（原样输出）** — 存规则片段、Surge 模块之类，通过直链原样对外提供。格式可选纯文本 / JavaScript / JSON / YAML / INI，保存前会按格式做真实校验（JSON/YAML 解析、INI 逐行检查、JS 编译）。\r
\r
**② Mihomo 配置（模板转换）** — 由所选订阅的节点套用模板，产出可直接订阅的配置。需要指定节点来源（单条订阅或组合），并选模板语言：\r
\r
| 模板语言 | 说明 |\r
|---|---|\r
| **YAML 覆写**（新建默认） | 在自动生成的基础配置上打补丁。深度合并：标量/对象递归，数组默认整体替换。支持 \`+key\` 前插、\`key+\` 追加、\`key!\` 强制覆盖 |\r
| **Go 模板** | 从零手写整份配置。可用 \`.Nodes\` 数组，每项含 \`Name\` \`Type\` \`Server\` \`Port\` \`UDP\` \`Extra\` |\r
| **JS 脚本覆写** | 必须定义 \`function main(config)\` 并 return，\`config\` 是自动生成的基础配置对象 |\r
\r
后两者的区别：Go 模板要自己写全 \`proxies\`/\`proxy-groups\`/\`rules\`；YAML 覆写与 JS 覆写是增量修改，对齐官方 Sub-Store 的「覆写」用法。\r
\r
> **兼容行为**：新建文件默认 YAML 覆写，但打开旧文件时若没记录模板语言，会兜底为 Go 模板——因为后端对存量数据也按 Go 模板解释。不要随手改成 YAML，那会静默改变旧文件的渲染结果。\r
\r
**正文来源**可以是本地编辑、远程拉取，或两者合并（本地在前 / 远程在前）。远程地址每行一个，以 \`#\` 开头的行被忽略便于临时停用。多个地址并发拉取但按书写顺序拼接。\r
\r
远程失败处理三选一：失败即报错（推荐）/ 跳过并提示 / 静默跳过。**选跳过时缺失的内容没有明显提示，客户端可能拿到不完整的配置。**\r
\r
文件直链是 \`/api/v1/file/<token>\`，**不支持 \`?target=\` 与 \`?filter=\`**（原样输出或固定模板渲染，没有这个概念）。\r
\r
### 分享管理\r
\r
集中管理订阅、组合、文件的对外链接。凭据在创建实体时就自动生成，这里做的是管理已有分享。\r
\r
| 操作 | 说明 |\r
|---|---|\r
| 设置 | 改分享展示名（留空显示来源名）、设有效期（快捷 7/30/90 天或永不过期） |\r
| 重置链接 | 生成新凭据，**旧链接立即失效**。用于凭据外泄后的补救 |\r
| 撤销 | 清空凭据，访问者立即无法获取。实体本身保留，可再「重新启用」 |\r
\r
状态徽标：生效中 / 已撤销 / 已过期 / 来源已停用。\r
\r
链接格式：\r
\r
\`\`\`\r
/api/v1/share/<token>              # 订阅与组合\r
/api/v1/share/<token>?target=surge # 指定格式\r
/api/v1/share/<token>?filter=香港   # 临时筛选\r
/api/v1/file/<token>               # 文件模板\r
\`\`\`\r
\r
**这些链接无需登录，凭据即链接本身。** 请勿在分享内容里放敏感信息；怀疑外泄就重置。\r
\r
---\r
\r
## 配置中心\r
\r
### 远程订阅来源\r
\r
远程层只能选一个来源：\r
\r
| 类型 | 说明 |\r
|---|---|\r
| 不使用（仅本地配置） | 默认。最终配置等于本地填的内容 |\r
| 本地订阅：单条 | 用某一条订阅 |\r
| 本地订阅：组合 | 用某个组合（推荐，能用上管道处理） |\r
| 本地文件模板 | 只能选「配置类型 = Mihomo 配置」的文件 |\r
| 外部订阅链接 | 别人分享给你的地址。不存为订阅、不参与定时刷新，每次合并都重新拉取 |\r
\r
停用的订阅/组合仍会列出但不可选，并标注原因——直接隐藏会让人困惑于「我明明建了这个组合，为什么选不到」。\r
\r
**定时拉取**：勾选后填 Cron，或用四个预设（每 30 分钟 / 每小时 / 每 6 小时 / 每天 4:00）。关闭则只能手动拉取。\r
\r
两个按钮的区别：**保存**只记来源设置；**立即拉取并合并**才会真的回源并重新生成最终配置。\r
\r
### 本地基础配置 11 组\r
\r
所有字段留空 = 不设置该项，由内核回落默认值。placeholder 显示的是官方默认，不是已生效的值。\r
\r
| 分组 | 管什么 |\r
|---|---|\r
| **通用设置** | 运行模式、日志级别、允许局域网、IPv6、进程匹配、TLS 指纹、TCP 并发、连接保活、入口认证（\`user:pass\` 每行一条）、局域网 IP 白/黑名单 |\r
| **端口设置** | \`port\` \`socks-port\` \`mixed-port\` \`redir-port\` \`tproxy-port\`。推荐只开 \`mixed-port\`。后两个是透明代理端口，别在这里填，见下方说明 |\r
| **外部控制** | \`external-controller\`（默认 \`127.0.0.1:9090\`）、\`secret\`、面板挂载路径。**对外暴露时务必设 secret** |\r
| **GeoData 规则库** | GeoIP/GeoSite 数据源与自动更新间隔 |\r
| **运行状态持久化** | 记住手选节点（建议开启）、记住 fake-ip 映射 |\r
| **域名解析** | DNS 全部行为：enhanced-mode（推荐 fake-ip）、fake-ip 段与过滤、各类 nameserver、respect-rules 等，以及**自定义 hosts 映射**（域名 → IP 的行编辑器，需同组的「使用 hosts 映射」开启才生效）。**用 TUN 时必须开 dns.enable** |\r
| **虚拟网卡** | TUN 的 enable / stack / auto-route / dns-hijack / mtu / strict-route 等 |\r
| **域名嗅探** | 从流量还原域名。注意：只开总开关不配 \`sniffer.sniff\` 的话嗅探不会生效 |\r
| **策略组** | YAML 数组定义本地策略组，与订阅中同名组合并 |\r
| **基础路由规则** | 每行一条，会被置顶插入最终配置，顺序即优先级 |\r
| **高级参数** | 兜底所有未建模的顶层键：\`listeners\` \`proxy-providers\` \`sub-rules\` \`tls\` \`experimental\` \`tunnels\` \`ntp\` 等。\`hosts\` 已移到「域名解析」的专属表单，写在这里会被忽略 |\r
\r
**两个必须知道的交互**：\r
\r
1. **清空「策略组」或「高级参数」编辑器不等于清空配置**。编辑过程中必然经过「空」这个中间态，若当场生效会把内容删光，所以空值被忽略。真要清空得点字段下方的「清空此项」按钮。\r
2. **数字字段清空会删除该键**，而不是写 0。因为 \`port: 0\` 与「不设置 port」语义完全不同。\r
\r
**关于端口设置里的两个透明代理端口**\r
\r
mihomo 有三条透明代理路径，本面板只一键支持其中两条：\r
\r
| 配置键 | UDP | 谁写防火墙规则 | 面板一键支持 |\r
|---|---|---|---|\r
| \`tun.enable\` | 支持 | mihomo 自己，退出时自动清理 | 支持，见「透明代理」页 |\r
| \`tproxy-port\` | 支持 | 本面板 | 支持，见「透明代理」页 |\r
| \`redir-port\` | **不支持** | **你自己** | 不支持 |\r
\r
所以：\r
\r
- **要一键接管局域网流量，去「透明代理」页开开关**，不要在端口设置里填这两个端口。\r
- \`tproxy-port\` 在端口设置里填了会被开关接管：开关选 TProxy 时实际端口取那一页的设置值，开关关闭时此处填的值会在生成配置时被清空。这是刻意的——开关状态必须与实际配置一致，否则界面显示与真实行为会脱节。\r
- \`redir-port\` 是留给想自己写 iptables \`nat\` 规则的高级用户的原生字段，面板不代管它的规则，也不会在退出时清理。它只支持 TCP，UDP 走不了（REDIRECT 是 nat 表的 TCP 目标，没有 UDP 等价物），QUIC / HTTP3 会直接直连泄漏。没有特殊理由不要用它。\r
\r
三个保存按钮：\r
\r
- **放弃修改（重新加载）** — 丢弃未保存改动\r
- **保存基础配置** — 只存本地层\r
- **保存并应用** — 存完立即重新生成最终配置（不拉远程）\r
\r
若基础配置加载失败，表单会整体隐藏并显示错误——这是防止用空表单覆盖服务端真实配置。\r
\r
### 合并策略与冲突\r
\r
在「系统设置 → 配置合并策略」设定本地与远程冲突时采用哪一方：\r
\r
| 冲突类型 | 可选策略 |\r
|---|---|\r
| 节点冲突 | 本地优先（默认）/ 远程优先 / 自动合并 / 手动确认 |\r
| 规则冲突 | 本地优先（默认）/ 远程优先 / 手动确认。**没有「自动合并」**，因为规则有先后顺序语义，无法安全地自动合并 |\r
| DNS 冲突 | 本地优先（默认）/ 远程优先 |\r
| 虚拟网卡冲突 | 本地优先（默认）/ 远程优先 |\r
| 其余通用参数 | 本地优先（默认）/ 远程优先 |\r
\r
「其余通用参数」覆盖运行模式、各监听端口、Geo 数据、外部控制、认证、嗅探等所有其他顶层项。\r
\r
**无论选哪种策略，订阅里没声明的项一定沿用你的本地设置，不会被清空。**\r
\r
> **重要限制**：选择「手动确认」后，控制台只会显示「N 项冲突待处理」的条数，**界面上没有逐条选择的入口**。除非你直接调用 API，否则建议不要用「手动确认」。\r
\r
---\r
\r
## 配置差异\r
\r
三栏只读对照：**本地配置** / **最终配置** / **远程订阅**，各自可下载成 yaml。\r
\r
排查「为什么我改的东西没生效」时先看这里：对比本地栏与最终栏，就知道是被远程覆盖了还是压根没保存。\r
\r
改动请回配置中心，本页只读。\r
\r
---\r
\r
## 运行日志\r
\r
两个标签：\r
\r
- **内核日志** — mihomo 子进程的输出，分 输出 / 错误 / 系统\r
- **应用日志** — 本程序自己的日志，可按级别筛选（错误 / 严重 / 慢调用 / 信息 / 调试）\r
\r
前端保留 500 行，自动跟随滚动；手动上滑会暂停跟随并提示，滑回底部恢复。\r
\r
「清空」只清当前标签。清应用日志时会同时清后端内存缓冲，**但不动磁盘上的日志文件**。\r
\r
磁盘日志在 \`data/logs/aurora.log\`，8MB × 5 份滚动，另有按天数的定时清理（见系统设置）。\r
\r
---\r
\r
## 系统设置\r
\r
**这一页有四个独立的提交入口**，不是「改完点一次保存」：\r
\r
| 区块 | 如何生效 |\r
|---|---|\r
| 管理员密码 | 自己的「修改密码」按钮 |\r
| 透明代理 | 开关即时生效 |\r
| 配置合并策略 | 自己的「保存合并策略」按钮 |\r
| 自动更新 / 下载出网 / 日志 | 页面底部的「保存设置」按钮 |\r
\r
### 管理员密码\r
\r
当前密码 + 新密码（至少 8 位）+ 确认。\r
\r
### 组件状态\r
\r
显示 mihomo 与 Zashboard 的安装状态、版本、路径，三个按钮：检查更新（只读版本不下载）、更新 Mihomo（有确认）、更新 Zashboard（有确认）。\r
\r
内核版本由 \`mihomo -v\` 探测。面板是纯静态资源，本地无法反查版本，只记录经本平台更新的那一次——手工放入或旧版本装的面板会显示「版本未知」。\r
\r
### 自动更新\r
\r
| 项 | 默认 | 说明 |\r
|---|---|---|\r
| 启用自动更新 | 关 | mihomo 与 Zashboard |\r
| Cron 表达式 | \`0 0 4 * * *\` | 支持 5 位或 6 位（6 位含秒） |\r
\r
### 透明代理\r
\r
让局域网设备无需各自设置代理即可分流。仅 Linux（TUN / TProxy）与 macOS（仅 TUN）可用，Windows 不支持。\r
\r
**两种模式怎么选**\r
\r
| | TUN | TProxy |\r
|---|---|---|\r
| 平台 | Linux / macOS | 仅 Linux |\r
| ICMP（ping 分流） | 支持 | 不支持 |\r
| 谁改防火墙 | mihomo 自己，退出时自动清理 | 本面板 |\r
| 风险 | 较低 | 较高，配错可能导致主机失联 |\r
\r
**默认选 TUN。** 它把路由与防火墙交给 mihomo 管，进程退出规则自动回收。TProxy 的意义是覆盖没有 TUN 设备的环境（宿主未加载 \`tun\` 模块、容器未映射 \`/dev/net/tun\` 且无法调整）。\r
\r
**启用流程**\r
\r
1. 打开开关。环境不具备条件时开关是灰的，展开下方各模式卡片能看到缺什么，以及可直接复制的安装命令\r
2. 确认弹窗会告知风险与 90 秒时限\r
3. 页面顶部出现琥珀色横幅与倒计时\r
4. **用另一台设备**访问本面板或目标网站，确认网络正常\r
5. 回来点「网络正常，确认」\r
\r
**没在 90 秒内确认会自动拆除规则并关闭开关。** 这不是多余的步骤——TProxy 规则配错时你会同时失去 SSH 与面板访问，而那时已经无法通过界面关掉开关。这个窗口是唯一的补救通道。\r
\r
回滚意图会写进数据库，面板崩溃或宿主重启后仍然有效。\r
\r
宿主重启还有另一种情况：你已经确认过、开关正常开着，但重启会把规则连同策略路由一起清空（这是 nftables 本身不持久化，不是程序的问题）。面板启动时会核实规则是否还在，发现没了就把开关回落成关闭并在日志里说明，不会让界面停在「显示开着、实际没接管」的状态。\r
\r
首次启用 TProxy 建议在有物理或控制台访问的机器上验证。\r
\r
**缺依赖时的自动准备**\r
\r
模式显示不可用、原因是缺工具时，卡片上会有「尝试自动准备环境」按钮。它做两件事，可以分别勾选：\r
\r
- **安装缺失的软件包**：按检测到的包管理器执行 \`apt-get install\` 或 \`apk add\`，装 \`iptables\`/\`nftables\`/\`iproute2\`\r
- **调整系统参数**：往 \`/etc/sysctl.d/99-auroramihomo.conf\` 写本程序需要的内核参数，然后加载它让参数立即生效。加载优先用 \`sysctl --system\`；Alpine 这类 BusyBox 环境不认这个选项，会自动回退成 \`sysctl -p <该文件>\`，两步都会在结果里如实列出\r
\r
几个刻意的限制，避免它变成一个乱动系统的黑箱：\r
\r
- 需要 root。不是 root 会直接拒绝并让你用手动命令，而不是跑一半失败留下装了一半的状态\r
- 只装写死在程序里的那几个包，不接受任何外部传入的命令或包名\r
- sysctl 只写它自己那个文件，不碰 \`/etc/sysctl.conf\`，也只写**确实不合规**的项（比如你有意关着转发，它不会顺手给你打开）\r
- 重复点没有副作用：包已装齐会跳过，配置文件是整体重写而不是往后追加\r
- 写文件与让参数生效是分开报告的两步。只看到「已写入」不等于已经生效，要看后面那步的结果——尤其 \`rp_filter\` 没真正改掉时，TProxy 会静默收不到包\r
- 不会碰防火墙规则，也不会顺手把透明代理开关打开——这两件事仍然要你自己操作\r
\r
执行完会逐步列出做了什么、成功还是失败，失败的直接给出命令原始输出（\`apt\` 报的是源不可达还是磁盘满，只有原文说得清）。跑完会自动重新检测一次，所以能立刻看到现在到底可用了没有。\r
\r
**手动命令始终可见**，不管自动执行成功与否。\r
\r
容器里有两点不同：装包能成功，但**容器重建后就没了**，界面会明确提示；sysctl 则完全不动——非特权容器会被内核拒绝，而 host 网络下会直接改到宿主上，这种事不该由面板替你决定。要长期用，改 Dockerfile 或用二进制部署。\r
\r
**环境检测详情**\r
\r
折叠区里的信息是排障依据。几个关键项：\r
\r
| 项 | 含义 |\r
|---|---|\r
| root / NET_ADMIN | 有没有改网络配置的权限 |\r
| TUN 设备 | \`/dev/net/tun\` 的实际路径，空则表示没找到 |\r
| 容器内 / host 网络 | 桥接网络里的规则只对容器自己生效，服务不了局域网其它设备 |\r
| \`NET_ADMIN(bounding)\` | 只在「已授予但当前进程未持有」时出现 |\r
\r
关于 \`rp_filter\` 那条告警有个坑：内核判定时取 \`all\` 与**具体网卡**两个值里的最大值，所以只把 \`all\` 改成 2，在那些自身还是 1 的网卡上照样丢包。检测会把这些网卡名一并列出来，自动准备也会连它们一起改。\r
\r
最后一项是容器部署最容易踩的坑：compose 里写了 \`cap_add: NET_ADMIN\` **不等于**进程真的持有该权限。非 root 用户运行且二进制没有 file capability 时，权限只在 bounding 集里，实际拿不到。解决办法是加 \`user: "0:0"\` 以 root 运行。\r
\r
容器启用透明代理需要四项改动，缺任何一项都会失败，且症状各不相同：\r
\r
| 缺失项 | 症状 |\r
|---|---|\r
| \`network_mode: host\` | 规则生效但局域网其它设备不受影响 |\r
| \`cap_add: NET_ADMIN\` | 检测报告 NET_ADMIN 为否，bounding 也是否 |\r
| \`devices: /dev/net/tun\` | 检测报告 TUN 设备未找到 |\r
| \`user: "0:0"\` | bounding 为是但 NET_ADMIN 为否 |\r
\r
TProxy 需要的 \`iptables\`/\`nftables\`/\`iproute2\` 已经预装在镜像里，不用自己进容器装。\r
\r
两种模式要的东西不一样，别搞混：\r
\r
| | 需要映射 \`/dev/net/tun\` | 需要 nft/iptables/iproute2 |\r
|---|---|---|\r
| TUN | 需要 | 不需要（mihomo 自己走 netlink 管规则） |\r
| TProxy | 不需要 | 需要（已预装） |\r
\r
所以上面那四项里，\`devices: /dev/net/tun\` 只有用 TUN 时才要加；只用 TProxy 的话不映射设备也能跑。\r
\r
**本机自己的流量也会走代理**\r
\r
容易被忽略的一点：**两种模式都会一并接管本程序所在这台机器自身的流量**，不只是局域网里其它设备的。开启后，宿主上的 \`curl\`、\`apt\`、\`git\` 都会按分流规则走节点。这是随模式启用的固定行为，没有单独的开关。\r
\r
有三点会影响本机流量的实际表现：\r
\r
**① 本机 DNS 可能没被劫持。** 如果 \`/etc/resolv.conf\` 的 \`nameserver\` 指向回环地址（\`127.0.0.53\` 是 systemd-resolved 的默认形态，Ubuntu/Debian 上很常见），本机的 DNS 查询不会经过 mihomo，**域名类分流规则对本机流量不生效**，只按 IP 分流。\r
\r
这是刻意的：mihomo 自己的 DNS 就监听在回环上，劫持回环会形成自环。环境检测会明确告警并带上具体地址。想让本机也按域名分流，二选一：\r
\r
\`\`\`bash\r
# 方式一：关掉 systemd-resolved 的 stub 监听\r
# 在 /etc/systemd/resolved.conf 里设 DNSStubListener=no，然后\r
systemctl restart systemd-resolved\r
# 方式二：把 resolv.conf 的 nameserver 直接改成非回环地址（如局域网 DNS）\r
\`\`\`\r
\r
局域网里其它设备不受这个限制。\r
\r
**② IPv6 只在这台机器确实能走 v6 时才接管。** 需要同时有全局 IPv6 地址和 IPv6 默认路由，两者齐备才会下发 v6 规则；否则只接管 IPv4 并给出告警。\r
\r
不是偷懒：只下发 v6 规则而没有配套的 v6 路由，v6 流量会被标记后无路可走，从「不分流」变成「不通」，比不接管更糟。\r
\r
**③ SSH、面板、内核 API 与本程序自己的出站始终直连。** 前三个是防止你把自己关在门外，最后一个是为了让「优先经由本地 Mihomo 代理出网」那个设置说话算数，也保证 mihomo 挂掉时本程序还能下载内核把它救回来。\r
\r
**让终端设备走代理**\r
\r
透明代理生效后，局域网设备还需要把流量导向运行本程序的主机。四种方式，按侵入性从低到高：\r
\r
**① 手动代理**（最简单，其实不需要透明代理）\r
\r
各设备自己填代理地址，不改任何网络设置。适合设备少或想先验证节点可用性。\r
\r
需要在「配置中心 → 通用设置」开启 \`allow-lan\`，并在端口设置里开 \`mixed-port\`（默认 7890）。\r
\r
- **macOS**：系统设置 → 网络 → 选中网卡 → 详细信息 → 代理\r
- **iOS / Android**：Wi-Fi → 当前网络 → 配置代理 → 手动，主机填面板主机 IP，端口 7890\r
- **Windows**：设置 → 网络和 Internet → 代理 → 手动设置代理\r
\r
局域网不可信时应配 \`authentication\` 与 \`lan-allowed-ips\`，否则同网段任何设备都能用你的代理。\r
\r
**② 只改 DNS**（改动小，分流能力有限）\r
\r
路由器 DHCP 只下发 DNS 指向面板主机，配合 \`enhanced-mode: fake-ip\` 与 \`dns-hijack\`。\r
\r
注意 **Android 的「私人 DNS」会绕过 DHCP 下发的 DNS**，必须在系统设置里关掉，否则分流不生效。\r
\r
**③ 网关模式**（把面板主机设为局域网网关）\r
\r
面板主机需要开转发：\r
\r
\`\`\`bash\r
sysctl -w net.ipv4.ip_forward=1\r
sysctl -w net.ipv6.conf.all.forwarding=1\r
# 持久化：写入 /etc/sysctl.d/99-aurora.conf\r
\`\`\`\r
\r
然后二选一：路由器 DHCP 把网关与 DNS 都指向面板主机，或各设备手动改。\r
\r
**④ 旁路由 / 单臂路由**\r
\r
面板主机只有一块网卡，与主路由同网段。主路由继续管 DHCP，但把网关与 DNS 指向面板主机；面板主机自己的网关仍指向主路由。\r
\r
三个常见陷阱：\r
\r
- **不对称路由**：终端发给旁路由、主路由却直接回包。需要 \`net.ipv4.conf.all.rp_filter=0\`，必要时做 MASQUERADE\r
- **ICMP 重定向**：主路由会告诉终端「绕过旁路由直连」，需要 \`net.ipv4.conf.all.send_redirects=0\`\r
- **DHCP 冲突**：不要在旁路由上再开一个 DHCP 服务\r
\r
**怎么验证真的生效了**\r
\r
别只看浏览器能否上网（可能命中缓存或直连规则）：\r
\r
\`\`\`bash\r
# 出口 IP 是否为节点 IP。在面板主机上跑就是验证本机接管，\r
# 在局域网其它设备上跑就是验证设备接管\r
curl -s https://api.ipify.org; echo\r
# DNS 是否被接管（fake-ip 模式下应返回 198.18.x.x 段）\r
nslookup www.google.com\r
\`\`\`\r
\r
在面板主机上验证时注意：如果检测告警说本机 DNS 指向回环，那么上面第二条在本机\r
返回的就不是 fake-ip 段（原因见「本机自己的流量也会走代理」①），\r
但第一条仍应显示节点 IP——按 IP 的分流照常生效。\r
\r
**已知限制**\r
\r
- TProxy 规则不持久化到重启。宿主重启后规则与策略路由都会消失，面板启动时会发现这一点并把开关回落成关闭，需要你重新开启一次。刻意不替你静默重新下发——那等于绕过了上面那个确认窗口\r
- macOS 只有 TUN，且必须以 root 运行（macOS 没有 capability 机制）\r
- 同时存在其它 VPN 的 TUN 设备时可能冲突\r
- 本机接管不能单独关掉，没有「只代理局域网设备」这个选项\r
- 本机 DNS 指向回环时，本机的域名分流不生效（局域网设备不受影响）\r
- 这台机器没有 IPv6 出网能力时只接管 IPv4，检测会告警说明\r
\r
### 下载与更新出网\r
\r
| 项 | 默认 | 说明 |\r
|---|---|---|\r
| 优先经由本地 Mihomo 代理出网 | 开 | 内核在跑时，下载与版本查询都先走它。拿到的是 GitHub 官方原始文件，不用担心第三方镜像返回被篡改或截断的内容 |\r
| 下载源（每行一个） | 8 个内置源 | 按顺序尝试，失败自动回退 |\r
\r
下载源的写法：完整前缀（如 \`https://mirror.example.com\`，会拼在官方地址前）或含 \`%s\` 的模板。**裸域名会被忽略**，jsdelivr 会被跳过（它只镜像仓库内文件，代理不了 Release 资产）。\r
\r
检查更新走 \`api.github.com\`，不使用这些源——GitHub REST API 没有可用镜像，网络不通时靠上面的 mihomo 代理兜底。\r
\r
### 日志\r
\r
| 项 | 默认 | 范围 |\r
|---|---|---|\r
| 日志保留天数 | 7 | 1–365 |\r
| 启用定时清理 | 开 | 关闭后只靠大小轮转（8MB × 5 份） |\r
| 清理时间（Cron） | \`0 30 3 * * *\` | 5 位或 6 位，保存后即时生效 |\r
\r
清理只影响已归档的历史文件，当前正在写的那份由大小轮转管理。\r
\r
---\r
\r
## Zashboard 面板\r
\r
内嵌的 mihomo 官方面板，可看实时连接、切换节点、测延迟。\r
\r
进入页面时后端会用当前内核的 \`external-controller\` 拼出带认证的地址，**面板自动对接，无需手填**。\r
\r
打不开的常见原因：\r
\r
- 内核没启用 \`external-controller\` → 去「配置中心 → 外部控制」设成 \`127.0.0.1:9090\` 后重新合并配置\r
- 面板资源没装 → 去「系统设置」执行「更新 Zashboard」\r
\r
也可通过 \`http://<地址>/ui/\` 直接访问。\r
\r
---\r
\r
## 配置文件与环境变量\r
\r
配置文件默认 \`backend/api/etc/aurora-api.yaml\`，容器内是 \`docker/aurora-api.docker.yaml\`。\r
\r
### 配置文件主要项\r
\r
| 项 | 默认 | 说明 |\r
|---|---|---|\r
| \`Host\` / \`Port\` | \`0.0.0.0\` / \`8899\` | 监听地址与端口 |\r
| \`Timeout\` | 300000 | 单请求处理超时（毫秒）。合并配置、下载内核远超默认 3 秒 |\r
| \`Server.ReadHeaderTimeoutSec\` | 10 | 防慢速连接（Slowloris）长期占用 |\r
| \`Server.ReadTimeoutSec\` | 60 | |\r
| \`Server.WriteTimeoutSec\` | 360 | **必须大于 \`Timeout\`**，否则长耗时请求会在写响应阶段被掐断 |\r
| \`Server.IdleTimeoutSec\` | 120 | |\r
| \`TrustedProxies\` | \`[]\` | 可信反代的 IP/CIDR。留空则一律用真实 RemoteAddr |\r
| \`DataSource\` | \`./data/aurora.db\` | SQLite 路径 |\r
| \`Mihomo.BinaryPath\` | 空 | 留空则用数据目录下的默认位置 |\r
| \`Mihomo.ConfigDir\` | \`./data\` | 数据目录 |\r
| \`AppLog.MemoryLimit\` | 1000 | 内存保留条数，供界面查看 |\r
| \`AppLog.ToFile\` | true | 落盘便于回溯重启前的现场 |\r
| \`AppLog.MaxFileMB\` / \`MaxBackups\` | 8 / 5 | 磁盘占用上界约 40MB |\r
| \`AppLog.IncludeAccessLog\` | false | 打开后满屏是访问记录，业务日志会被冲走。排查请求链路时再临时开 |\r
| \`Updater.UseMihomoProxy\` | true | 优先经本地内核代理出网 |\r
| \`Updater.CDNProviders\` | 8 个源 | 顺序即优先级 |\r
\r
**\`TrustedProxies\` 的两个坑**：部署在反代之后却不填，所有请求会被归到反代 IP 上（一个用户登录失败会影响其他人）；填成 \`0.0.0.0/0\` 则等于允许任何人伪造头部绕过限流。\r
\r
### 环境变量（优先级高于配置文件）\r
\r
| 变量 | 说明 |\r
|---|---|\r
| \`AURORA_JWT_SECRET\` | JWT 签名密钥。生产建议显式设置，否则首次启动随机生成并存库 |\r
| \`AURORA_JWT_EXPIRE\` | 令牌有效期（秒），默认 86400 |\r
| \`AURORA_DATA_SOURCE\` | SQLite 路径 |\r
| \`AURORA_CONFIG_DIR\` | 数据目录 |\r
| \`AURORA_MIHOMO_BINARY\` | mihomo 二进制路径 |\r
| \`AURORA_HOST\` / \`AURORA_PORT\` | 监听地址与端口 |\r
| \`AURORA_AUTO_UPDATE\` | 是否启用自动更新（true/false） |\r
| \`AURORA_AUTO_UPDATE_CRON\` | 自动更新 cron（6 段，含秒） |\r
| \`AURORA_GITHUB_API\` | GitHub API 地址，可指向自建镜像 |\r
| \`AURORA_CDN_PROVIDERS\` | CDN 源列表，逗号分隔 |\r
| \`AURORA_USE_MIHOMO_PROXY\` | 是否优先经内核代理出网 |\r
\r
---\r
\r
## 常见问题\r
\r
**改了配置为什么没生效？**\r
\r
去「配置差异」页对比本地栏与最终栏。三种可能：没点「保存并应用」；被远程层覆盖了（调整合并策略）；内核没重载（去内核管理点「重载配置」）。\r
\r
**订阅刷新了但节点没变？**\r
\r
订阅的「刷新缓存」只更新自身缓存，不改最终配置。要进内核得去配置中心「立即拉取并合并」。\r
\r
**分享链接打不开？**\r
\r
检查四点：分享是否被撤销、是否已过期、来源订阅/组合是否被停用、链接是否完整（凭据很长，容易复制不全）。\r
\r
**策略组/高级参数被我清空了，保存后却还在？**\r
\r
这是有意的保护。清空编辑器不生效，要点字段下方的「清空此项」按钮。\r
\r
**内核下载一直失败？**\r
\r
先在「系统设置 → 下载与更新出网」调整源顺序。仍然不行就手工放置内核：\r
\r
\`\`\`bash\r
# 从 https://github.com/MetaCubeX/mihomo/releases 下载对应平台的包\r
# 注意 Linux/macOS 官方发的是 .gz（gzip 压缩的裸二进制，不是 tar 归档），\r
# 只有 Windows 是 .zip\r
gunzip -c mihomo-linux-amd64-v1.19.29.gz > <数据目录>/bin/mihomo\r
chmod +x <数据目录>/bin/mihomo\r
\`\`\`\r
\r
数据目录默认是程序旁边的 \`data/\`，容器内是 \`/data\`。放好后到「内核管理」点启动。\r
\r
**mihomo 官方发布的 Linux 包是什么格式？**\r
\r
\`.gz\`（gzip 压缩的裸二进制，不是 tar 归档），只有 Windows 是 \`.zip\`。手工解压用 \`gunzip -c xxx.gz > mihomo\`。\r
\r
**TUN 开了但局域网设备没走代理？**\r
\r
容器部署最常见的是网络模式问题——桥接网络里的规则只对容器自己生效，必须用 \`network_mode: host\`。另外查「系统设置 → 透明代理 → 环境检测详情」，它会明确指出缺 capability、缺设备还是缺 host 网络。\r
\r
**忘记管理员密码？**\r
\r
没有内置的重置命令，需要手工操作数据库：删掉密码记录后重启，程序会重新生成初始密码并写入数据目录下的 \`initial_password.txt\`。\r
\r
\`\`\`bash\r
# 先停服务，SQLite 正在写入时改动可能损坏数据\r
sqlite3 <数据目录>/aurora.db "delete from settings where key='admin_password';"\r
# 重启后读新密码\r
cat <数据目录>/initial_password.txt\r
\`\`\`\r
\r
宿主没装 \`sqlite3\` 时可用任意 SQLite 客户端，或直接从备份恢复。\r
`,Dr={class:`p-4 sm:p-6 lg:p-8`},Or={class:`mx-auto max-w-7xl`},kr=[`aria-expanded`],Ar={class:`flex flex-col gap-6 lg:flex-row lg:items-start`},jr={class:`space-y-0.5 text-sm`},Mr=[`aria-current`,`onClick`],Nr={key:0,class:`absolute inset-y-1 left-0 w-0.5 rounded-full bg-accent-text`,"aria-hidden":`true`},Pr=[`innerHTML`],Fr=m(p({__name:`DocsView`,setup(i){let p=e=>e.trim().toLowerCase().replace(/[^\w\u4e00-\u9fa5 -]/g,``).replace(/\s+/g,`-`);function m(e){let t=e.split(`
`),n=t.findIndex(e=>/^##\s+目录\s*$/.test(e));if(n<0)return e;let r=n+1;for(;r<t.length&&!/^#{1,2}\s/.test(t[r]);)r++;return[...t.slice(0,n),...t.slice(r)].join(`
`)}function g(){let e=[],t=new Map,n=new $({html:!1,linkify:!0,typographer:!1});n.renderer.rules.heading_open=(n,r)=>{let i=n[r].tag,a=Number(i.slice(1)),o=n[r+1]?.content??``,s=p(o),c=t.get(s)??0;return t.set(s,c+1),c>0&&(s=`${s}-${c}`),(a===2||a===3)&&e.push({level:a,text:o,id:s}),`<${i} id="${s}">`},n.renderer.rules.table_open=()=>`<div class="doc-table-wrap"><table>`,n.renderer.rules.table_close=()=>`</table></div>`;let r=n.renderer.rules.link_open||((e,t,n,r,i)=>i.renderToken(e,t,n));return n.renderer.rules.link_open=(e,t,n,i,a)=>{let o=e[t].attrGet(`href`)??``;return/^https?:/.test(o)&&(e[t].attrSet(`target`,`_blank`),e[t].attrSet(`rel`,`noopener noreferrer`)),r(e,t,n,i,a)},{html:n.render(m(Er)),toc:e}}let{html:_,toc:v}=g(),y=r(``),b=r(null),x=null;t(()=>{requestAnimationFrame(()=>{let e=b.value;if(!e)return;let t=e.querySelectorAll(`h2[id], h3[id]`);t.length!==0&&(x=new IntersectionObserver(e=>{let t=e.filter(e=>e.isIntersecting).sort((e,t)=>e.boundingClientRect.top-t.boundingClientRect.top);t.length>0&&(y.value=t[0].target.id)},{rootMargin:`-80px 0px -70% 0px`,threshold:0}),t.forEach(e=>x.observe(e)))})}),s(()=>x?.disconnect());let S=e=>{let t=document.getElementById(e);t&&(t.scrollIntoView({behavior:`smooth`,block:`start`}),y.value=e)},C=r(!1);return(t,r)=>(h(),e(`main`,Dr,[f(`div`,Or,[r[1]||=f(`div`,{class:`mb-4 flex flex-wrap items-center justify-between gap-2`},[f(`h1`,{class:`text-2xl font-bold sm:text-3xl`},`使用文档`),f(`p`,{class:`text-xs text-fg-subtle`},` 随程序内置，离线可用，版本与当前程序一致。 `)],-1),f(`button`,{class:`btn btn-secondary mb-3 w-full lg:hidden`,"aria-expanded":C.value,"aria-controls":`doc-toc`,onClick:r[0]||=e=>C.value=!C.value},a(C.value?`收起目录`:`展开目录`)+`（`+a(d(v).length)+` 节） `,9,kr),f(`div`,Ar,[f(`nav`,{id:`doc-toc`,class:c([`thin-scrollbar shrink-0 rounded-lg border border-line bg-surface p-2 lg:sticky lg:top-6 lg:w-64 lg:max-h-[calc(100dvh-6rem)] lg:overflow-y-auto`,C.value?`block`:`hidden lg:block`]),"aria-label":`文档目录`},[f(`ul`,jr,[(h(!0),e(l,null,n(d(v),t=>(h(),e(`li`,{key:t.id},[f(`button`,{class:c([`relative block w-full rounded-md py-1.5 pr-2 text-left transition-colors`,t.level===3?`pl-6 text-xs`:`pl-3 font-medium`,y.value===t.id?`bg-accent-solid/10 font-semibold text-accent-text dark:bg-accent-solid/20`:t.level===3?`text-fg-muted hover:bg-elevated hover:text-fg`:`text-fg hover:bg-elevated`]),"aria-current":y.value===t.id?`location`:void 0,onClick:e=>{S(t.id),C.value=!1}},[y.value===t.id?(h(),e(`span`,Nr)):o(``,!0),u(` `+a(t.text),1)],10,Mr)]))),128))])],2),f(`article`,{ref_key:`contentEl`,ref:b,class:`doc-body min-w-0 flex-1 rounded-lg border border-line bg-surface p-4 shadow sm:p-6`,innerHTML:d(_)},null,8,Pr)])])]))}}),[[`__scopeId`,`data-v-78816f5b`]]);export{Fr as default};