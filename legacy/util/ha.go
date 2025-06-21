package util

type HaAdvert struct {
	Base string `json:"~"`
	Dev HaAdvertDev `json:"dev"`
	{
		"~":"esp-cam/motion/hobby_room_1",
		"dev":{
			"name":"hobby_room_1",
			"ids":["hobby_room_1"],
			"cns":[["ip","192.168.176.232"]],
			"mdl":"camera_firmware",
			"sw":"motioncam 52546c6"
		},
		"o":{
			"name":"esp-idf","sw":"v5.3.1-638-ga0f798cfc4"
		},
		"avty":[{"t":"~/status/operational",
			"val_tpl":"{{ 'offline' if value == 'Disconnected' else 'online' }}"
			}],
		"name":"motion",
		"dev_cla":"motion",
		"uniq_id":"hobby_room_1-motion",
		"stat_t":"~/event/motion"
	}
}

type HaAdvertDev struct {
	
}