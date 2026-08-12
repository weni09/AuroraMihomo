package substore

import "strings"

// regionKeywords 各区域的常见命名关键词（含中文/英文/缩写/emoji 旗帜/主要城市）。
//
// 匹配语义（keyword.go matchKeyword）：
//   - 中文、emoji、较长英文单词：直接子串匹配；
//   - 两字母缩写（<=3 字符 ASCII）：按词边界匹配，避免 "India" 里的 "in"、
//     "Beijing" 里的 "be" 这类单词内部片段被误判。
//
// 因此中文关键词尽量用全称或明确词（不用单字），缩写可放心加。
//
// 城市名按国家归入对应关键词：机场节点常写城市（"洛杉矶-01"、"东京-01"），
// 城市只属于一个国家，子串匹配安全。
var regionKeywords = map[string][]string{
	// ---- 东亚 / 东南亚 ----
	"HK": {"hk", "hong kong", "hongkong", "香港", "港", "🇭🇰"},
	"TW": {"tw", "taiwan", "台湾", "台", "台北", "taipei", "高雄", "kaohsiung", "🇹🇼"},
	"JP": {"jp", "japan", "日本", "东京", "tokyo", "大阪", "osaka", "名古屋", "nagoya", "札幌", "sapporo", "冲绳", "okinawa", "福冈", "fukuoka", "🇯🇵"},
	"KR": {"kr", "korea", "韩国", "首尔", "seoul", "釜山", "busan", "🇰🇷"},
	"SG": {"sg", "singapore", "新加坡", "狮城", "🇸🇬"},
	"MY": {"my", "malaysia", "马来西亚", "吉隆坡", "kuala lumpur", "槟城", "penang", "🇲🇾"},
	"ID": {"id", "indonesia", "印尼", "印度尼西亚", "雅加达", "jakarta", "巴厘", "bali", "🇮🇩"},
	"TH": {"th", "thailand", "泰国", "曼谷", "bangkok", "芭提雅", "pattaya", "清迈", "chiang mai", "🇹🇭"},
	"VN": {"vn", "vietnam", "越南", "河内", "hanoi", "胡志明", "ho chi minh", "西贡", "saigon", "🇻🇳"},
	"PH": {"ph", "philippines", "菲律宾", "马尼拉", "manila", "🇵🇭"},
	"MM": {"mm", "myanmar", "缅甸", "🇲🇲"},
	"KH": {"kh", "cambodia", "柬埔寨", "金边", "phnom penh", "🇰🇭"},
	// 不加 "la" 缩写：会与洛杉矶（美国）冲突
	"LA": {"laos", "老挝", "🇱🇦"},
	"BN": {"bn", "brunei", "文莱", "🇧🇳"},
	"NP": {"np", "nepal", "尼泊尔", "🇳🇵"},
	"MN": {"mn", "mongolia", "蒙古", "🇲🇳"},
	"MO": {"mo", "macau", "macao", "澳门", "🇲🇴"},
	"CN": {"cn", "china", "中国", "回国", "上海", "shanghai", "北京", "beijing", "广州", "guangzhou", "深圳", "shenzhen", "🇨🇳"},
	// north korea 的 "korea" 会被 KR 先命中（regionOrder 顺序），中文/缩写可正常匹配
	"KP": {"kp", "north korea", "朝鲜", "🇰🇵"},

	// ---- 南亚 / 西亚 / 中亚 ----
	"IN": {"in", "india", "印度", "孟买", "mumbai", "新德里", "delhi", "班加罗尔", "bangalore", "🇮🇳"},
	"TR": {"tr", "turkey", "土耳其", "伊斯坦布尔", "istanbul", "安卡拉", "ankara", "🇹🇷"},
	"SA": {"sa", "saudi", "沙特", "利雅得", "riyadh", "🇸🇦"},
	"IL": {"il", "israel", "以色列", "特拉维夫", "tel aviv", "🇮🇱"},
	"AE": {"ae", "uae", "dubai", "阿联酋", "迪拜", "阿布扎比", "abu dhabi", "🇦🇪"},
	"QA": {"qa", "qatar", "卡塔尔", "多哈", "doha", "🇶🇦"},
	"KW": {"kw", "kuwait", "科威特", "🇰🇼"},
	"OM": {"om", "oman", "阿曼", "🇴🇲"},
	"BH": {"bh", "bahrain", "巴林", "🇧🇭"},
	"JO": {"jo", "jordan", "约旦", "🇯🇴"},
	"LB": {"lb", "lebanon", "黎巴嫩", "🇱🇧"},
	"IQ": {"iq", "iraq", "伊拉克", "🇮🇶"},
	"IR": {"ir", "iran", "伊朗", "🇮🇷"},
	"PK": {"pk", "pakistan", "巴基斯坦", "🇵🇰"},
	"BD": {"bd", "bangladesh", "孟加拉", "🇧🇩"},
	"LK": {"lk", "sri lanka", "斯里兰卡", "🇱🇰"},
	"KZ": {"kz", "kazakhstan", "哈萨克", "🇰🇿"},
	"UZ": {"uz", "uzbekistan", "乌兹别克", "🇺🇿"},
	"GE": {"ge", "georgia", "格鲁吉亚", "🇬🇪"},
	"AM": {"am", "armenia", "亚美尼亚", "🇦🇲"},
	"AZ": {"az", "azerbaijan", "阿塞拜疆", "🇦🇿"},

	// ---- 欧洲 ----
	// 不加 "gb" 缩写：订阅信息节点常写「剩余流量：xxx GB」，会误加英国旗。
	// 英国节点用 uk / 英国 / 伦敦 / england 等已足够。
	"UK": {"uk", "united kingdom", "britain", "英国", "英格兰", "england", "伦敦", "london", "曼彻斯特", "manchester", "🇬🇧"},
	"RU": {"ru", "russia", "俄罗斯", "莫斯科", "moscow", "圣彼得堡", "st petersburg", "🇷🇺"},
	"DE": {"de", "germany", "德国", "法兰克福", "frankfurt", "柏林", "berlin", "慕尼黑", "munich", "汉堡", "hamburg", "杜塞尔多夫", "dusseldorf", "🇩🇪"},
	"FR": {"fr", "france", "法国", "巴黎", "paris", "马赛", "marseille", "里昂", "lyon", "🇫🇷"},
	"IT": {"it", "italy", "意大利", "米兰", "milan", "罗马", "rome", "威尼斯", "venice", "🇮🇹"},
	"ES": {"es", "spain", "西班牙", "马德里", "madrid", "巴塞罗那", "barcelona", "🇪🇸"},
	"PT": {"pt", "portugal", "葡萄牙", "里斯本", "lisbon", "波尔图", "porto", "🇵🇹"},
	"NL": {"nl", "netherlands", "荷兰", "阿姆斯特丹", "amsterdam", "鹿特丹", "rotterdam", "🇳🇱"},
	"BE": {"be", "belgium", "比利时", "布鲁塞尔", "brussels", "🇧🇪"},
	"CH": {"ch", "switzerland", "瑞士", "苏黎世", "zurich", "日内瓦", "geneva", "🇨🇭"},
	"AT": {"at", "austria", "奥地利", "维也纳", "vienna", "🇦🇹"},
	"SE": {"se", "sweden", "瑞典", "斯德哥尔摩", "stockholm", "🇸🇪"},
	"NO": {"no", "norway", "挪威", "奥斯陆", "oslo", "🇳🇴"},
	"DK": {"dk", "denmark", "丹麦", "哥本哈根", "copenhagen", "🇩🇰"},
	"FI": {"fi", "finland", "芬兰", "赫尔辛基", "helsinki", "🇫🇮"},
	"IE": {"ie", "ireland", "爱尔兰", "都柏林", "dublin", "🇮🇪"},
	"PL": {"pl", "poland", "波兰", "华沙", "warsaw", "🇵🇱"},
	"CZ": {"cz", "czech", "捷克", "布拉格", "prague", "🇨🇿"},
	"SK": {"sk", "slovakia", "斯洛伐克", "🇸🇰"},
	"HU": {"hu", "hungary", "匈牙利", "布达佩斯", "budapest", "🇭🇺"},
	"RO": {"ro", "romania", "罗马尼亚", "布加勒斯特", "bucharest", "🇷🇴"},
	"BG": {"bg", "bulgaria", "保加利亚", "索菲亚", "sofia", "🇧🇬"},
	"GR": {"gr", "greece", "希腊", "雅典", "athens", "🇬🇷"},
	"HR": {"hr", "croatia", "克罗地亚", "🇭🇷"},
	"RS": {"rs", "serbia", "塞尔维亚", "贝尔格莱德", "belgrade", "🇷🇸"},
	"UA": {"ua", "ukraine", "乌克兰", "基辅", "kiev", "kyiv", "🇺🇦"},
	"BY": {"by", "belarus", "白俄罗斯", "🇧🇾"},
	"LT": {"lt", "lithuania", "立陶宛", "🇱🇹"},
	"LV": {"lv", "latvia", "拉脱维亚", "🇱🇻"},
	"EE": {"ee", "estonia", "爱沙尼亚", "🇪🇪"},
	"SI": {"si", "slovenia", "斯洛文尼亚", "🇸🇮"},
	"IS": {"is", "iceland", "冰岛", "雷克雅未克", "reykjavik", "🇮🇸"},
	"LU": {"lu", "luxembourg", "卢森堡", "🇱🇺"},
	"MT": {"mt", "malta", "马耳他", "🇲🇹"},
	"CY": {"cy", "cyprus", "塞浦路斯", "🇨🇾"},
	"AL": {"al", "albania", "阿尔巴尼亚", "🇦🇱"},
	"MK": {"mk", "north macedonia", "北马其顿", "🇲🇰"},
	"BA": {"ba", "bosnia", "波黑", "波斯尼亚", "🇧🇦"},
	"MD": {"md", "moldova", "摩尔多瓦", "🇲🇩"},
	"ME": {"me", "montenegro", "黑山", "🇲🇪"},
	"GI": {"gi", "gibraltar", "直布罗陀", "🇬🇮"},

	// ---- 美洲 ----
	"US": {"us", "usa", "united states", "america", "美国", "洛杉矶", "los angeles", "纽约", "new york", "旧金山", "san francisco", "西雅图", "seattle", "达拉斯", "dallas", "芝加哥", "chicago", "迈阿密", "miami", "波士顿", "boston", "拉斯维加斯", "las vegas", "亚特兰大", "atlanta", "华盛顿", "washington", "圣何塞", "san jose", "凤凰城", "phoenix", "休斯顿", "houston", "丹佛", "denver", "🇺🇸"},
	"CA": {"ca", "canada", "加拿大", "多伦多", "toronto", "温哥华", "vancouver", "蒙特利尔", "montreal", "渥太华", "ottawa", "卡尔加里", "calgary", "🇨🇦"},
	"MX": {"mx", "mexico", "墨西哥", "墨西哥城", "mexico city", "🇲🇽"},
	"BR": {"br", "brazil", "巴西", "圣保罗", "sao paulo", "里约", "rio", "🇧🇷"},
	"AR": {"ar", "argentina", "阿根廷", "布宜诺斯艾利斯", "buenos aires", "🇦🇷"},
	"CL": {"cl", "chile", "智利", "圣地亚哥", "santiago", "🇨🇱"},
	"CO": {"co", "colombia", "哥伦比亚", "波哥大", "bogota", "🇨🇴"},
	"PE": {"pe", "peru", "秘鲁", "利马", "lima", "🇵🇪"},
	"VE": {"ve", "venezuela", "委内瑞拉", "加拉加斯", "caracas", "🇻🇪"},
	"UY": {"uy", "uruguay", "乌拉圭", "蒙得维的亚", "montevideo", "🇺🇾"},
	"EC": {"ec", "ecuador", "厄瓜多尔", "基多", "quito", "🇪🇨"},
	"BO": {"bo", "bolivia", "玻利维亚", "🇧🇴"},
	"PY": {"py", "paraguay", "巴拉圭", "🇵🇾"},
	"PA": {"pa", "panama", "巴拿马", "🇵🇦"},
	"CR": {"cr", "costa rica", "哥斯达黎加", "🇨🇷"},
	"GT": {"gt", "guatemala", "危地马拉", "🇬🇹"},
	"HN": {"hn", "honduras", "洪都拉斯", "🇭🇳"},
	"SV": {"sv", "el salvador", "萨尔瓦多", "🇸🇻"},
	"NI": {"ni", "nicaragua", "尼加拉瓜", "🇳🇮"},
	"DO": {"do", "dominican", "多米尼加", "🇩🇴"},
	"CU": {"cu", "cuba", "古巴", "🇨🇺"},
	"JM": {"jm", "jamaica", "牙买加", "🇯🇲"},
	"TT": {"tt", "trinidad", "特立尼达", "🇹🇹"},
	"PR": {"pr", "puerto rico", "波多黎各", "🇵🇷"},
	"KY": {"ky", "cayman", "开曼", "🇰🇾"},
	"BM": {"bm", "bermuda", "百慕大", "🇧🇲"},
	"BS": {"bs", "bahamas", "巴哈马", "🇧🇸"},
	"BB": {"bb", "barbados", "巴巴多斯", "🇧🇧"},

	// ---- 非洲 ----
	"ZA": {"za", "south africa", "南非", "约翰内斯堡", "johannesburg", "开普敦", "cape town", "🇿🇦"},
	"EG": {"eg", "egypt", "埃及", "开罗", "cairo", "🇪🇬"},
	"MA": {"ma", "morocco", "摩洛哥", "卡萨布兰卡", "casablanca", "🇲🇦"},
	"NG": {"ng", "nigeria", "尼日利亚", "拉各斯", "lagos", "🇳🇬"},
	"KE": {"ke", "kenya", "肯尼亚", "内罗毕", "nairobi", "🇰🇪"},
	"ET": {"et", "ethiopia", "埃塞俄比亚", "🇪🇹"},
	"TZ": {"tz", "tanzania", "坦桑尼亚", "🇹🇿"},
	"GH": {"gh", "ghana", "加纳", "🇬🇭"},
	"DZ": {"dz", "algeria", "阿尔及利亚", "🇩🇿"},
	"TN": {"tn", "tunisia", "突尼斯", "🇹🇳"},
	"CI": {"ci", "ivory coast", "cote d'ivoire", "科特迪瓦", "🇨🇮"},
	"ZW": {"zw", "zimbabwe", "津巴布韦", "🇿🇼"},
	"MZ": {"mz", "mozambique", "莫桑比克", "🇲🇿"},
	"AO": {"ao", "angola", "安哥拉", "🇦🇴"},
	"ZM": {"zm", "zambia", "赞比亚", "🇿🇲"},
	"UG": {"ug", "uganda", "乌干达", "🇺🇬"},
	"SN": {"sn", "senegal", "塞内加尔", "🇸🇳"},
	"CM": {"cm", "cameroon", "喀麦隆", "🇨🇲"},
	"MG": {"mg", "madagascar", "马达加斯加", "🇲🇬"},
	"RW": {"rw", "rwanda", "卢旺达", "🇷🇼"},
	"BW": {"bw", "botswana", "博茨瓦纳", "🇧🇼"},
	"NA": {"na", "namibia", "纳米比亚", "🇳🇦"},
	"LR": {"lr", "liberia", "利比里亚", "🇱🇷"},

	// ---- 大洋洲 / 其它 ----
	"AU": {"au", "australia", "澳大利亚", "澳洲", "悉尼", "sydney", "墨尔本", "melbourne", "布里斯班", "brisbane", "珀斯", "perth", "🇦🇺"},
	"NZ": {"nz", "new zealand", "新西兰", "奥克兰", "auckland", "惠灵顿", "wellington", "🇳🇿"},
	"FJ": {"fj", "fiji", "斐济", "🇫🇯"},
	"PG": {"pg", "papua new guinea", "巴布亚", "🇵🇬"},
	"GU": {"gu", "guam", "关岛", "🇬🇺"},
	"SC": {"sc", "seychelles", "塞舌尔", "🇸🇨"},
	"MU": {"mu", "mauritius", "毛里求斯", "🇲🇺"},
	"MV": {"mv", "maldives", "马尔代夫", "🇲🇻"},
	"RE": {"re", "reunion", "留尼汪", "🇷🇪"},
}

// regionOrder 固定地区匹配顺序。
// map 遍历顺序随机，若直接遍历 regionKeywords，
// 同一份订阅每次执行可能得到不同的国旗结果。
// 既有国家保持原顺序在前（优先级不变），新增国家按区域分组追加。
var regionOrder = []string{
	// 既有（保持优先级不变）
	"HK", "TW", "JP", "SG", "KR", "US", "UK",
	"DE", "FR", "CA", "AU", "RU", "IN", "TR", "NL",
	"MY", "ID", "AR", "NZ", "AE", "BE", "IT",
	// 亚洲
	"TH", "VN", "PH", "SA", "IL", "QA", "KW", "OM", "BH", "JO", "LB",
	"IQ", "IR", "PK", "BD", "LK", "MM", "KH", "LA", "BN", "NP", "MN",
	"KZ", "UZ", "GE", "AM", "AZ", "CN", "MO", "KP",
	// 欧洲
	"ES", "PT", "CH", "AT", "SE", "NO", "DK", "FI", "IE", "PL", "CZ",
	"SK", "HU", "RO", "BG", "GR", "HR", "RS", "UA", "BY", "LT", "LV",
	"EE", "SI", "IS", "LU", "MT", "CY", "AL", "MK", "BA", "MD", "ME", "GI",
	// 美洲
	"MX", "BR", "CL", "CO", "PE", "VE", "UY", "EC", "BO", "PY", "PA",
	"CR", "GT", "HN", "SV", "NI", "DO", "CU", "JM", "TT", "PR", "KY",
	"BM", "BS", "BB",
	// 非洲
	"ZA", "EG", "MA", "NG", "KE", "ET", "TZ", "GH", "DZ", "TN", "CI",
	"ZW", "MZ", "AO", "ZM", "UG", "SN", "CM", "MG", "RW", "BW", "NA", "LR",
	// 大洋洲与其它
	"FJ", "PG", "GU", "SC", "MU", "MV", "RE",
}

// regionFlags 各地区对应的国旗 emoji
var regionFlags = map[string]string{
	"HK": "🇭🇰", "TW": "🇹🇼", "JP": "🇯🇵", "SG": "🇸🇬",
	"US": "🇺🇸", "KR": "🇰🇷", "UK": "🇬🇧", "DE": "🇩🇪",
	"FR": "🇫🇷", "CA": "🇨🇦", "AU": "🇦🇺", "RU": "🇷🇺",
	"IN": "🇮🇳", "TR": "🇹🇷", "NL": "🇳🇱",
	"MY": "🇲🇾", "ID": "🇮🇩", "AR": "🇦🇷", "NZ": "🇳🇿",
	"AE": "🇦🇪", "BE": "🇧🇪", "IT": "🇮🇹",
	"TH": "🇹🇭", "VN": "🇻🇳", "PH": "🇵🇭", "SA": "🇸🇦",
	"IL": "🇮🇱", "QA": "🇶🇦", "KW": "🇰🇼", "OM": "🇴🇲",
	"BH": "🇧🇭", "JO": "🇯🇴", "LB": "🇱🇧", "IQ": "🇮🇶",
	"IR": "🇮🇷", "PK": "🇵🇰", "BD": "🇧🇩", "LK": "🇱🇰",
	"MM": "🇲🇲", "KH": "🇰🇭", "LA": "🇱🇦", "BN": "🇧🇳",
	"NP": "🇳🇵", "MN": "🇲🇳", "MO": "🇲🇴", "CN": "🇨🇳",
	"KP": "🇰🇵", "KZ": "🇰🇿", "UZ": "🇺🇿", "GE": "🇬🇪",
	"AM": "🇦🇲", "AZ": "🇦🇿",
	"ES": "🇪🇸", "PT": "🇵🇹", "CH": "🇨🇭", "AT": "🇦🇹",
	"SE": "🇸🇪", "NO": "🇳🇴", "DK": "🇩🇰", "FI": "🇫🇮",
	"IE": "🇮🇪", "PL": "🇵🇱", "CZ": "🇨🇿", "SK": "🇸🇰",
	"HU": "🇭🇺", "RO": "🇷🇴", "BG": "🇧🇬", "GR": "🇬🇷",
	"HR": "🇭🇷", "RS": "🇷🇸", "UA": "🇺🇦", "BY": "🇧🇾",
	"LT": "🇱🇹", "LV": "🇱🇻", "EE": "🇪🇪", "SI": "🇸🇮",
	"IS": "🇮🇸", "LU": "🇱🇺", "MT": "🇲🇹", "CY": "🇨🇾",
	"AL": "🇦🇱", "MK": "🇲🇰", "BA": "🇧🇦", "MD": "🇲🇩",
	"ME": "🇲🇪", "GI": "🇬🇮",
	"MX": "🇲🇽", "BR": "🇧🇷", "CL": "🇨🇱", "CO": "🇨🇴",
	"PE": "🇵🇪", "VE": "🇻🇪", "UY": "🇺🇾", "EC": "🇪🇨",
	"BO": "🇧🇴", "PY": "🇵🇾", "PA": "🇵🇦", "CR": "🇨🇷",
	"GT": "🇬🇹", "HN": "🇭🇳", "SV": "🇸🇻", "NI": "🇳🇮",
	"DO": "🇩🇴", "CU": "🇨🇺", "JM": "🇯🇲", "TT": "🇹🇹",
	"PR": "🇵🇷", "KY": "🇰🇾", "BM": "🇧🇲", "BS": "🇧🇸", "BB": "🇧🇧",
	"ZA": "🇿🇦", "EG": "🇪🇬", "MA": "🇲🇦", "NG": "🇳🇬",
	"KE": "🇰🇪", "ET": "🇪🇹", "TZ": "🇹🇿", "GH": "🇬🇭",
	"DZ": "🇩🇿", "TN": "🇹🇳", "CI": "🇨🇮", "ZW": "🇿🇼",
	"MZ": "🇲🇿", "AO": "🇦🇴", "ZM": "🇿🇲", "UG": "🇺🇬",
	"SN": "🇸🇳", "CM": "🇨🇲", "MG": "🇲🇬", "RW": "🇷🇼",
	"BW": "🇧🇼", "NA": "🇳🇦", "LR": "🇱🇷",
	"FJ": "🇫🇯", "PG": "🇵🇬", "GU": "🇬🇺", "SC": "🇸🇨",
	"MU": "🇲🇺", "MV": "🇲🇻", "RE": "🇷🇪",
}

// SupportedRegions 返回所有可用区域代码
func SupportedRegions() []string {
	out := make([]string, 0, len(regionOrder))
	return append(out, regionOrder...)
}

// matchRegion 判断节点名是否属于指定区域
func matchRegion(name, region string) bool {
	kws, ok := regionKeywords[strings.ToUpper(strings.TrimSpace(region))]
	if !ok {
		return false
	}
	lower := strings.ToLower(name)
	for _, kw := range kws {
		if matchKeyword(lower, kw) {
			return true
		}
	}
	return false
}

// applyRegionFilter 按区域保留/剔除节点（Sub-Store: Region Filter）
// payload.action: keep | drop
// payload.regions: []string，如 ["HK","JP"]
func applyRegionFilter(nodes []Node, payload map[string]interface{}) ([]Node, error) {
	action, _ := payload["action"].(string)
	if action == "" {
		action = "keep"
	}

	raw, _ := payload["regions"].([]interface{})
	regions := make([]string, 0, len(raw))
	for _, r := range raw {
		if s, ok := r.(string); ok && strings.TrimSpace(s) != "" {
			regions = append(regions, s)
		}
	}
	if len(regions) == 0 {
		return nodes, nil
	}

	out := make([]Node, 0, len(nodes))
	for _, n := range nodes {
		matched := false
		for _, region := range regions {
			if matchRegion(n.Name, region) {
				matched = true
				break
			}
		}
		if action == "drop" && matched {
			continue
		}
		if action == "keep" && !matched {
			continue
		}
		out = append(out, n)
	}
	return out, nil
}
